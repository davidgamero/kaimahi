# kaimahi-proxy: static binary on distroless (CA certs included for the
# keyed Copilot upstream; nonroot by default).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /kaimahi-proxy ./cmd/kaimahi-proxy

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /kaimahi-proxy /kaimahi-proxy
ENTRYPOINT ["/kaimahi-proxy"]
