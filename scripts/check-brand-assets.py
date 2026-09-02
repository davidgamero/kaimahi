#!/usr/bin/env python3
from __future__ import annotations

import struct
import sys
import xml.etree.ElementTree as ET
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

PNG_REQUIREMENTS = {
    "brand/mascot.png": (1536, 1536, True),
    "brand/mark.png": (1024, 1024, True),
    "brand/hero.png": (1600, 600, False),
    "brand/social-preview.png": (1280, 640, False),
}

SVG_REQUIREMENTS = {
    "brand/mark.svg": "Kaimahi compact mark",
    "brand/wordmark.svg": "Kaimahi wordmark",
    "docs/assets/architecture.svg": "Kaimahi governance architecture",
}

PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"
PNG_BIT_DEPTHS = {
    0: {1, 2, 4, 8, 16},
    2: {8, 16},
    3: {1, 2, 4, 8},
    4: {8, 16},
    6: {8, 16},
}
PNG_CHANNELS = {0: 1, 2: 3, 3: 1, 4: 2, 6: 4}
SVG_NAMESPACE = "http://www.w3.org/2000/svg"


def png_scanline_layout(
    width: int, height: int, bits_per_pixel: int, interlace: int
) -> list[tuple[int, int]]:
    passes = [(0, 0, 1, 1)]
    if interlace:
        passes = [
            (0, 0, 8, 8),
            (4, 0, 8, 8),
            (0, 4, 4, 8),
            (2, 0, 4, 4),
            (0, 2, 2, 4),
            (1, 0, 2, 2),
            (0, 1, 1, 2),
        ]

    layout = []
    for x_start, y_start, x_step, y_step in passes:
        pass_width = (width - x_start + x_step - 1) // x_step if width > x_start else 0
        pass_height = (
            (height - y_start + y_step - 1) // y_step if height > y_start else 0
        )
        if pass_width and pass_height:
            row_bytes = (pass_width * bits_per_pixel + 7) // 8
            layout.append((pass_height, row_bytes))
    return layout


def png_metadata(path: Path) -> tuple[int, int, bool]:
    data = path.read_bytes()
    if not data.startswith(PNG_SIGNATURE):
        raise ValueError("invalid PNG signature")

    offset = len(PNG_SIGNATURE)
    ihdr = None
    idat_parts = []
    saw_plte = False
    idat_ended = False
    saw_iend = False
    chunk_number = 0

    while offset < len(data):
        if len(data) - offset < 12:
            raise ValueError("truncated PNG chunk")
        length = struct.unpack(">I", data[offset : offset + 4])[0]
        chunk_type = data[offset + 4 : offset + 8]
        chunk_end = offset + 12 + length
        if chunk_end > len(data):
            raise ValueError("truncated PNG chunk")
        if not all(
            byte in range(ord("A"), ord("Z") + 1)
            or byte in range(ord("a"), ord("z") + 1)
            for byte in chunk_type
        ):
            raise ValueError("invalid PNG chunk type")
        if chunk_type[2:3].islower():
            raise ValueError("invalid PNG reserved chunk bit")

        chunk_data = data[offset + 8 : offset + 8 + length]
        stored_crc = struct.unpack(">I", data[offset + 8 + length : chunk_end])[0]
        actual_crc = zlib.crc32(chunk_type + chunk_data) & 0xFFFFFFFF
        chunk_name = chunk_type.decode("ascii")
        if stored_crc != actual_crc:
            raise ValueError(f"invalid {chunk_name} CRC")

        if chunk_number == 0 and chunk_type != b"IHDR":
            raise ValueError("IHDR must be the first PNG chunk")
        if chunk_type == b"IHDR":
            if chunk_number != 0 or ihdr is not None:
                raise ValueError("duplicate or misplaced IHDR chunk")
            if length != 13:
                raise ValueError("IHDR length must be 13")
            ihdr = struct.unpack(">IIBBBBB", chunk_data)
            width, height, depth, color_type, compression, filtering, interlace = ihdr
            if not width or not height or width > 0x7FFFFFFF or height > 0x7FFFFFFF:
                raise ValueError("invalid IHDR dimensions")
            if color_type not in PNG_BIT_DEPTHS or depth not in PNG_BIT_DEPTHS[color_type]:
                raise ValueError("invalid IHDR color type or bit depth")
            if compression != 0 or filtering != 0 or interlace not in {0, 1}:
                raise ValueError("invalid IHDR compression, filter, or interlace method")
        elif chunk_type == b"PLTE":
            if saw_plte or idat_parts or length == 0 or length % 3 or length > 768:
                raise ValueError("invalid or misplaced PLTE chunk")
            if ihdr is not None and ihdr[3] in {0, 4}:
                raise ValueError("PLTE is not permitted for this PNG color type")
            if ihdr is not None and ihdr[3] == 3 and length // 3 > 1 << ihdr[2]:
                raise ValueError("PLTE has too many entries for the PNG bit depth")
            saw_plte = True
        elif chunk_type == b"IDAT":
            if idat_ended:
                raise ValueError("IDAT chunks must be consecutive")
            idat_parts.append(chunk_data)
        elif chunk_type == b"IEND":
            if length:
                raise ValueError("IEND chunk must be empty")
            if not idat_parts:
                raise ValueError("PNG has no IDAT chunk")
            saw_iend = True
            if chunk_end != len(data):
                raise ValueError("data follows IEND chunk")
        elif chunk_type[:1].isupper():
            raise ValueError(f"unknown critical PNG chunk {chunk_name}")

        if idat_parts and chunk_type not in {b"IDAT", b"IEND"}:
            idat_ended = True
        offset = chunk_end
        chunk_number += 1
        if saw_iend:
            break

    if ihdr is None:
        raise ValueError("PNG has no IHDR chunk")
    if not saw_iend:
        raise ValueError("PNG has no IEND chunk")

    width, height, depth, color_type, _compression, _filtering, interlace = ihdr
    if color_type == 3 and not saw_plte:
        raise ValueError("indexed PNG has no PLTE chunk")

    layout = png_scanline_layout(
        width, height, depth * PNG_CHANNELS[color_type], interlace
    )
    expected_size = sum(rows * (row_bytes + 1) for rows, row_bytes in layout)

    # Decompress at most one byte past the size the header implies, so an
    # over-long stream is rejected before it is materialized in full.
    compressed = b"".join(idat_parts)
    try:
        decompressor = zlib.decompressobj()
        image_data = decompressor.decompress(compressed, expected_size + 1)
        if len(image_data) > expected_size:
            raise ValueError("invalid PNG image data length")
        image_data += decompressor.flush()
    except zlib.error as error:
        raise ValueError("invalid PNG image data") from error
    if not decompressor.eof or decompressor.unused_data or decompressor.unconsumed_tail:
        raise ValueError("invalid PNG image data stream")
    if len(image_data) != expected_size:
        raise ValueError("invalid PNG image data length")
    position = 0
    for rows, row_bytes in layout:
        for _ in range(rows):
            if image_data[position] > 4:
                raise ValueError("invalid PNG scanline filter")
            position += row_bytes + 1

    return width, height, color_type in {4, 6}


def validate_png(relative: str, expected: tuple[int, int, bool]) -> list[str]:
    path = ROOT / relative
    if not path.is_file():
        return [f"{relative}: missing"]
    try:
        actual = png_metadata(path)
    except (OSError, ValueError, struct.error) as error:
        return [f"{relative}: {error}"]
    width, height, must_have_alpha = expected
    problems = []
    if actual[:2] != (width, height):
        problems.append(f"{relative}: dimensions {actual[:2]} != {(width, height)}")
    if must_have_alpha and not actual[2]:
        problems.append(f"{relative}: alpha channel required")
    return problems


def validate_svg(relative: str, expected_title: str) -> list[str]:
    path = ROOT / relative
    if not path.is_file():
        return [f"{relative}: missing"]
    try:
        root = ET.parse(path).getroot()
    except (OSError, ET.ParseError) as error:
        return [f"{relative}: invalid XML: {error}"]
    problems = []
    svg_tag = f"{{{SVG_NAMESPACE}}}svg"
    if root.tag not in {"svg", svg_tag}:
        problems.append(f"{relative}: root element is not svg")
    if not root.attrib.get("viewBox"):
        problems.append(f"{relative}: viewBox required")
    title_tag = f"{{{SVG_NAMESPACE}}}title" if root.tag == svg_tag else "title"
    titles = [
        node.text.strip()
        for node in root.iter()
        if node.tag == title_tag and node.text
    ]
    if expected_title not in titles:
        problems.append(f"{relative}: title must be exactly {expected_title!r}")
    return problems


def main() -> int:
    problems = []
    for relative, expected in PNG_REQUIREMENTS.items():
        problems.extend(validate_png(relative, expected))
    for relative, expected_title in SVG_REQUIREMENTS.items():
        problems.extend(validate_svg(relative, expected_title))
    brand_readme = ROOT / "brand/README.md"
    if not brand_readme.is_file():
        problems.append("brand/README.md: missing")
    if problems:
        print("brand asset validation failed:", file=sys.stderr)
        for problem in problems:
            print(f"- {problem}", file=sys.stderr)
        return 1
    print("brand assets: dimensions, alpha, and SVG metadata valid")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
