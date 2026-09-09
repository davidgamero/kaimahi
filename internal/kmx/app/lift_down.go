package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/lift"
)

// LiftDown removes what the lift created, and the two branches have opposite
// rules rather than different degrees of the same rule.
//
// On a cluster this path CREATED, everything went into one resource group and
// all of it comes out with the group — and the proof is that the group is
// afterwards gone.
//
// On a cluster somebody else created, the cluster and its resource group are
// never deleted and never adopted. Only the resources this run added come
// out, only by the id recorded when they went in, and anything whose id
// cannot be re-resolved is LEFT and named rather than removed on a guess.
// Deleting by name on a subscription we do not own is how a demo removes a
// stranger's production monitoring; the ordering of those two failures is not
// close, but leaving a resource quietly billing is bad enough that it is
// reported loudly rather than mentioned.
func (a *App) LiftDown(opt lift.Options) error {
	if err := opt.ValidateForTeardown(); err != nil {
		return err
	}
	if err := a.preflight(depAz, depKubectl); err != nil {
		return err
	}
	acct, err := a.azAccount()
	if err != nil {
		return err
	}
	record, err := a.readLiftRecord(opt.ResourceGroup, opt.Cluster)
	if err != nil {
		// No record is not the same as nothing to do, and it must not read
		// as success. On the created branch the resource group is still
		// removable by hand; on the other, resources may be billing with no
		// account of what they are.
		return fmt.Errorf(`%w

  Without the record there is no list of ids this run created, and this
  refuses to go looking by name — on a subscription that is not ours, a name
  can belong to somebody else. If you know a lift ran here, remove what it
  made by hand from the Azure portal, or delete the whole resource group if
  you created it for this.`, err)
	}
	if record.Subscription != acct.ID {
		return fmt.Errorf("kmx lift down: the record was written against a different subscription than the CLI is signed in to. Refusing to delete anything (az account set --subscription ...)")
	}
	if record.Branch != opt.Branch() {
		return fmt.Errorf("kmx lift down: this lift was recorded as %q and you asked for %q teardown. These have opposite rules, so the difference is refused rather than reconciled", record.Branch, opt.Branch())
	}

	if record.Branch == lift.BringYourOwn {
		return a.liftDownBringYourOwn(opt, record)
	}
	return a.liftDownCreated(opt, record)
}

// liftDownCreated hands the whole resource group to the script that already
// knows how to delete one safely: it refuses a group that does not carry the
// tag this path stamps, takes a confirmation naming the group, waits for the
// delete rather than reporting the request, and re-checks that the group is
// gone before saying so.
func (a *App) liftDownCreated(opt lift.Options, record *lift.Record) error {
	fmt.Fprintf(a.Err, "\nkmx lift down: this will irreversibly delete resource group %q and its contents,\n"+
		"  plus any outside resources listed in this run's record.\n", opt.ResourceGroup)
	if err := a.confirmLiftDown(opt); err != nil {
		return err
	}
	work, cleanup, err := a.liftWorkspace()
	if err != nil {
		return err
	}
	defer cleanup()

	if outside := record.Outside(); len(outside) > 0 {
		// A group deletion proves cleanup only for what was inside it. If a
		// resource landed elsewhere, say so before the group goes, while its
		// id is still on screen.
		fmt.Fprintln(a.Err, "\nkmx lift down: these were created OUTSIDE the resource group, so deleting")
		fmt.Fprintln(a.Err, "  the group will not remove them. Each is removed and checked separately:")
		for _, res := range outside {
			fmt.Fprintf(a.Err, "    %s %s\n", res.Kind, res.Name)
		}
		if err := a.removeRecorded(outside); err != nil {
			return err
		}
	}

	if err := a.runScript(work, "scripts/aks-down.sh", map[string]string{
		"AKS_RESOURCE_GROUP": opt.ResourceGroup,
		"AKS_CLUSTER":        opt.Cluster,
		// Go already verified consent for this group before ANY deletion.
		// Runner does not forward stdin; the script must not prompt again.
		"KAIMAHI_CONFIRM": opt.ResourceGroup,
	}); err != nil {
		return err
	}

	// The script already re-checks, and this checks again from here rather
	// than trusting an exit status: the claim being made is "it is gone",
	// and an unreadable answer is not that claim.
	state, err := a.groupExists(opt.ResourceGroup)
	switch {
	case err != nil, state == lift.Unusable:
		return fmt.Errorf("kmx lift down: the delete returned, but the resource group's state could not be re-checked — NOT claiming it is gone. If it is still there it is still billing:\n    az group exists --name %s", shellArg(opt.ResourceGroup))
	case state == lift.Present:
		return fmt.Errorf("kmx lift down: resource group %s still exists after the delete returned", opt.ResourceGroup)
	}
	a.forgetLiftRecord(opt)
	fmt.Fprintf(a.Err, "\nkmx lift down: resource group %s is gone (az group exists says false).\n"+
		"  Cleanup covers that group and the outside resources in this run's record; other resources and billing were not checked.\n", opt.ResourceGroup)
	return nil
}

// liftDownBringYourOwn removes the monitoring this run added to a cluster it
// does not own, and nothing else.
func (a *App) liftDownBringYourOwn(opt lift.Options, record *lift.Record) error {
	fmt.Fprintf(a.Err, `----------------------------------------------------------------
  kmx lift down — on a cluster YOU created

  cluster %q and resource group %q are NOT deleted. They were
  not created here and they are not deleted here.

  What comes out is only what this run put in, by the id it recorded:
`, opt.Cluster, opt.ResourceGroup)
	for _, res := range record.Created {
		fmt.Fprintf(a.Err, "    %s %s\n", res.Kind, res.Name)
	}
	fmt.Fprintln(a.Err, "----------------------------------------------------------------")

	if err := a.confirmLiftDown(opt); err != nil {
		return err
	}

	// The add-ons are turned off first. They hold references to the
	// workspaces, and a workspace deleted while something still routes to it
	// leaves the cluster reporting an error nobody asked for.
	a.aimAtTheCluster(opt)

	// Only add-ons THIS RUN enabled are turned off. An operator who already
	// had Container Insights running would otherwise have it switched off by a
	// teardown that was only ever meant to remove what the lift added — the
	// same mistake as deleting their workspace, made quieter by the fact that
	// nothing disappears, it just stops collecting.
	if record.Before.WeEnabledMetrics() {
		a.notef("turning off the Managed Prometheus this run enabled")
		if err := a.Run.Run("az", "aks", "update", "--name", opt.Cluster, "--resource-group", opt.ResourceGroup,
			"--disable-azure-monitor-metrics", "--output", "none"); err != nil {
			return fmt.Errorf("could not disable Managed Prometheus on your cluster — stopping before deleting anything it still points at: %w", err)
		}
	} else {
		a.notef("Managed Prometheus was already on before this run, or its prior state was not established; leaving it unchanged.")
	}
	if record.Before.WeEnabledLogs() {
		a.notef("turning off the Container Insights this run enabled")
		if err := a.Run.Run("az", "aks", "disable-addons", "--name", opt.Cluster, "--resource-group", opt.ResourceGroup,
			"--addons", "monitoring", "--output", "none"); err != nil {
			return fmt.Errorf("could not disable Container Insights on your cluster — stopping before deleting anything it still points at: %w", err)
		}
	} else {
		a.notef("Container Insights was already on before this run, or its prior state was not established; leaving it unchanged.")
	}

	// The in-cluster objects, removed on the same rule: only what this run
	// created, decided by what it recorded at the time and never by what the
	// object contains now.
	if err := a.removeInClusterObservability(record); err != nil {
		return fmt.Errorf("kmx lift down: in-cluster cleanup is incomplete; the run record has been kept.\n  Retry: %s\n%w", a.liftCommand(opt, true), err)
	}

	if err := a.removeRecorded(record.Created); err != nil {
		return err
	}
	if !record.Before.Recorded {
		a.notef("in-cluster monitoring ownership was not established; cleanup was not checked. The run record has been kept.")
		return nil
	}
	a.forgetLiftRecord(opt)
	return nil
}

// removeInClusterObservability takes back the two cluster-side objects — and
// only the ones this run created.
//
// Ownership comes from what the run RECORDED before it applied anything, never
// from what the object looks like now. A ConfigMap holding only our scrape job
// may still have been created by the operator, and "it looks like ours" is not
// "we made it"; the same goes for a NetworkPolicy of that name they had
// already written themselves.
func (a *App) removeInClusterObservability(record *lift.Record) error {
	var cleanupErr error
	if record.Before.WeCreatedScraperPolicy() {
		if err := a.kubectlRun("-n", "kaimahi", "delete", "networkpolicy", scraperPolicy, "--ignore-not-found"); err != nil {
			cleanupErr = fmt.Errorf("could not remove the scraper's NetworkPolicy allowance: %w\n"+
				"    kubectl --context %s -n kaimahi delete networkpolicy %s", err, shellArg(a.Cfg.KubeContext), scraperPolicy)
		}
	} else {
		a.notef("the NetworkPolicy %s was there before this run, or its origin was never established; leaving it.", scraperPolicy)
	}

	if !record.Before.WeCreatedScrapeConfig() {
		a.notef("%s in %s was there before this run, or its origin was never established; leaving it.\n"+
			"  If you merged this run's job into it, remove the kaimahi-plane job by hand.",
			scrapeConfigMap, scrapeConfigNamespace)
		return cleanupErr
	}
	// Created by this run, so ours to remove. The contents are still read —
	// not to establish ownership, but because an operator may have added
	// their own jobs to it since, and taking those with it would be the same
	// destruction by a slower route.
	body, err := a.kubectlCapture("-n", scrapeConfigNamespace, "get", "configmap", scrapeConfigMap,
		"-o", "jsonpath={.data.prometheus-config}")
	switch {
	case err == nil:
	case isNotFound(err):
		return cleanupErr // genuinely not there; nothing to take back
	default:
		// An unreachable API server, a missing context or an RBAC denial is
		// not "the ConfigMap is absent". Returning silently on those left the
		// scrape job in place with nobody told, which on a cluster we do not
		// own is a leftover the operator never hears about.
		return errors.Join(cleanupErr, fmt.Errorf("could not read %s in %s (%w) — it may still carry this run's scrape job.\n"+
			"  Check by hand: kubectl --context %s -n %s get configmap %s",
			scrapeConfigMap, scrapeConfigNamespace, err, shellArg(a.Cfg.KubeContext), scrapeConfigNamespace, scrapeConfigMap))
	}
	if strings.Contains(body, "job_name: kaimahi-plane") && strings.Count(body, "job_name:") == 1 {
		if err := a.kubectlRun("-n", scrapeConfigNamespace, "delete", "configmap", scrapeConfigMap, "--ignore-not-found"); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("could not remove %s in %s: %w", scrapeConfigMap, scrapeConfigNamespace, err))
		}
		return cleanupErr
	}
	return errors.Join(cleanupErr, fmt.Errorf("%s in %s no longer contains only the expected scrape job, so it is left alone; cleanup could not be verified.\n"+
		"  Review it and remove the kaimahi-plane job by hand:\n"+
		"    kubectl --context %s -n %s edit configmap %s", scrapeConfigMap, scrapeConfigNamespace,
		shellArg(a.Cfg.KubeContext), scrapeConfigNamespace, scrapeConfigMap))
}

// removeRecorded deletes recorded resources by their recorded id, confirming
// each one first, and reports everything it did not remove.
func (a *App) removeRecorded(resources []lift.Resource) error {
	if len(resources) == 0 {
		fmt.Fprintln(a.Err, "\nkmx lift down: no Azure resource ids were recorded; no Azure resources were checked or removed.")
		return nil
	}
	var removals []lift.Removal
	deleted, alreadyGone := 0, 0
	for _, res := range resources {
		state, resolved := a.confirmRecordedResource(res.ID)
		rm := lift.PlanRemoval(res, state, resolved)
		switch {
		case rm.Delete:
			if err := a.Run.Run("az", "resource", "delete", "--ids", res.ID, "--output", "none"); err != nil {
				rm.Delete = false
				rm.Reason = fmt.Sprintf("the delete failed and it may still be billing: %v", err)
			} else {
				deleted++
				a.notef("removed %s %s", res.Kind, res.Name)
			}
		case state == lift.Absent:
			// Turning the add-ons off takes their own rules and rule groups
			// with them, so several recorded resources are legitimately gone
			// before we reach them. Counted rather than passed over in
			// silence: "we deleted twelve things" and "six were already gone"
			// are different claims, and only one of them is true.
			alreadyGone++
			a.notef("already gone, nothing to delete: %s %s", res.Kind, res.Name)
		}
		removals = append(removals, rm)
	}

	left := lift.LeftBehind(removals)
	if len(left) == 0 {
		fmt.Fprintf(a.Err, "\nkmx lift down: recorded Azure resources: %d delete operations completed, %d already gone. Unrecorded resources were not checked.\n",
			deleted, alreadyGone)
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "kmx lift down: %d resource(s) were NOT removed, and they may still be billing.\n\n", len(left))
	for _, rm := range left {
		// PlanRemoval's diagnostic contains a raw command; quote its id here.
		rm.Reason = strings.ReplaceAll(rm.Reason, "az resource show --ids "+rm.Resource.ID, "az resource show --ids "+shellArg(rm.Resource.ID))
		fmt.Fprintf(&b, "  %s %q\n    %s\n", rm.Resource.Kind, rm.Resource.Name, rm.Reason)
		if rm.Resource.Billing != "" {
			fmt.Fprintf(&b, "    cost while it exists: %s\n", rm.Resource.Billing)
		}
		fmt.Fprintf(&b, "    id: %s\n\n", rm.Resource.ID)
	}
	b.WriteString("  Nothing was deleted on a guess. Check each id above and remove it by hand\n")
	b.WriteString("  if it is yours. The run record has been kept so this can be re-run.")
	return errors.New(b.String())
}

func (a *App) confirmLiftDown(opt lift.Options) error {
	name, kind := opt.Cluster, "cluster"
	if !opt.BringYourOwn {
		name, kind = opt.ResourceGroup, "resource group"
	}
	proceed := fmt.Sprintf("  to proceed:  KAIMAHI_CONFIRM=%s %s", shellArg(name), a.liftCommand(opt, true))
	if c := strings.TrimSpace(a.Cfg.Confirm); c != "" {
		if c == name {
			return nil
		}
		return fmt.Errorf("kmx lift down: KAIMAHI_CONFIRM does not name this %s — refusing.\n%s", kind, proceed)
	}
	if a.Stdin == nil || !isTerminalFile(a.Stdin) {
		return fmt.Errorf("kmx lift down: no TTY and no KAIMAHI_CONFIRM — refusing to act unattended on a cloud subscription.\n%s", proceed)
	}
	fmt.Fprintf(a.Err, "Type the %s name to confirm teardown (anything else aborts): ", kind)
	if readTrimmedLine(a.Stdin) != name {
		return errors.New("kmx lift down: not confirmed — nothing was deleted")
	}
	return nil
}

// forgetLiftRecord removes the record once everything in it is gone. It is
// only ever called after a complete removal: a record deleted while resources
// survive would take with it the only list of what they are.
func (a *App) forgetLiftRecord(opt lift.Options) {
	path, err := liftRecordPath(opt.ResourceGroup, opt.Cluster)
	if err != nil {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		a.notef("could not remove the run record %s: %v", path, err)
	}
}
