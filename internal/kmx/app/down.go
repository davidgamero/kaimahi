package app

import "fmt"

// Down deletes the kind cluster `kmx up` created.
//
// The guard vouches for KUBE_CTX; this deletes KIND_CLUSTER. They agree by
// default, but both are overridable, so `KUBE_CTX=kind-safe
// KIND_CLUSTER=other kmx down` would show a banner naming one cluster and
// destroy another. Refuse when the thing confirmed is not the thing deleted.
func (a *App) Down() error {
	want := "kind-" + a.Cfg.KindCluster
	if a.Cfg.KubeContext != want {
		return fmt.Errorf("refusing: the guard would check context %s, but this would\n"+
			"delete kind cluster %q (context %s).\n"+
			"Set KIND_CLUSTER and KUBE_CTX consistently, or just KIND_CLUSTER.",
			a.Cfg.KubeContext, a.Cfg.KindCluster, want)
	}
	if err := a.Guard(fmt.Sprintf("DELETE the kind cluster %q", a.Cfg.KindCluster), "kmx down"); err != nil {
		return err
	}
	return a.Run.Run("kind", "delete", "cluster", "--name", a.Cfg.KindCluster)
}
