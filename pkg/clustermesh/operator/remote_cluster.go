// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package operator

import (
	"context"
	"log/slog"
	"path"

	"k8s.io/utils/ptr"

	"github.com/cilium/cilium/api/v1/models"
	"github.com/cilium/cilium/pkg/clustermesh/common"
	mcsapitypes "github.com/cilium/cilium/pkg/clustermesh/mcsapi/types"
	serviceStore "github.com/cilium/cilium/pkg/clustermesh/store"
	"github.com/cilium/cilium/pkg/clustermesh/types"
	"github.com/cilium/cilium/pkg/clustermesh/wait"
	"github.com/cilium/cilium/pkg/kvstore"
	"github.com/cilium/cilium/pkg/kvstore/store"
	"github.com/cilium/cilium/pkg/lock"
)

// remoteCluster implements the clustermesh business logic on top of
// common.RemoteCluster.
type remoteCluster struct {
	logger *slog.Logger
	// name is the name of the cluster
	name string

	clusterMeshEnableEndpointSync bool
	clusterMeshEnableMCSAPI       bool

	// remoteServices is the shared store representing services in remote clusters
	remoteServices store.WatchStore
	// remoteServiceExports is the shared store representing service exports in remote clusters
	remoteServiceExports store.WatchStore

	storeFactory store.Factory

	clusterAddHooks    []func(string)
	clusterDeleteHooks []func(string)

	// status is the function which fills the common part of the status.
	status common.StatusFunc

	// synced tracks the initial synchronization with the remote cluster.
	synced synced
}

func (rc *remoteCluster) Run(ctx context.Context, backend kvstore.BackendOperations, config types.CiliumClusterConfig, ready chan<- error) {
	var mgr store.WatchStoreManager
	if config.Capabilities.SyncedCanaries {
		mgr = rc.storeFactory.NewWatchStoreManager(backend, rc.name)
	} else {
		mgr = store.NewWatchStoreManagerImmediate(rc.logger)
	}

	adapter := func(prefix string) string { return prefix }
	if config.Capabilities.Cached {
		adapter = kvstore.StateToCachePrefix
	}

	if rc.clusterMeshEnableEndpointSync {
		mgr.Register(adapter(serviceStore.ServiceStorePrefix), func(ctx context.Context) {
			rc.remoteServices.Watch(ctx, backend, path.Join(adapter(serviceStore.ServiceStorePrefix), rc.name))
		})
	}

	if rc.clusterMeshEnableMCSAPI && config.Capabilities.ServiceExportsEnabled != nil {
		mgr.Register(adapter(mcsapitypes.ServiceExportStorePrefix), func(ctx context.Context) {
			rc.remoteServiceExports.Watch(ctx, backend, path.Join(adapter(mcsapitypes.ServiceExportStorePrefix), rc.name))
		})
	} else {
		// Drain the remote service exports in case the remote cluster no longer supports them
		rc.remoteServiceExports.Drain()
		// Mimic that service exports are synced if not enabled
		rc.synced.serviceExports.Stop()
	}

	close(ready)
	for _, clusterAddHook := range rc.clusterAddHooks {
		clusterAddHook(rc.name)
	}
	mgr.Run(ctx)
}

func (rc *remoteCluster) Stop() {
	rc.synced.Stop()
}

func (rc *remoteCluster) Remove(context.Context) {
	for _, clusterDeleteHook := range rc.clusterDeleteHooks {
		clusterDeleteHook(rc.name)
	}
	// Draining shall occur only when the configuration for the remote cluster
	// is removed, and not in case the operator is shutting down, otherwise we
	// would break existing connections on restart.
	rc.remoteServices.Drain()
	rc.remoteServiceExports.Drain()
}

type synced struct {
	wait.SyncedCommon
	services       *lock.StoppableWaitGroup
	serviceExports *lock.StoppableWaitGroup
}

func newSynced() synced {
	return synced{
		SyncedCommon:   wait.NewSyncedCommon(),
		services:       lock.NewStoppableWaitGroup(),
		serviceExports: lock.NewStoppableWaitGroup(),
	}
}

// Services returns after that the initial list of shared services has been
// received from the remote cluster, the remote cluster is disconnected,
// or the given context is canceled.
func (s *synced) Services(ctx context.Context) error {
	return s.Wait(ctx, s.services.WaitChannel())
}

// ServiceExports returns after that the initial list of service exports has been
// received from the remote cluster, the remote cluster is disconnected,
// or the given context is canceled.
func (s *synced) ServiceExports(ctx context.Context) error {
	return s.Wait(ctx, s.serviceExports.WaitChannel())
}

func (rc *remoteCluster) Status() *models.RemoteCluster {
	status := rc.status()

	status.NumSharedServices = int64(rc.remoteServices.NumEntries())
	status.NumServiceExports = int64(rc.remoteServiceExports.NumEntries())

	status.Synced = &models.RemoteClusterSynced{
		Services: !rc.clusterMeshEnableEndpointSync || rc.remoteServices.Synced(),
		// The operator does not watch nodes, endpoints and identities, hence
		// let's pretend them to be synchronized by default.
		Nodes:      true,
		Endpoints:  true,
		Identities: true,
	}
	if status.Config != nil && status.Config.ServiceExportsEnabled != nil &&
		rc.clusterMeshEnableMCSAPI {
		status.Synced.ServiceExports = ptr.To(rc.remoteServiceExports.Synced())
	}

	status.Ready = status.Ready &&
		status.Synced.Nodes && status.Synced.Services &&
		(status.Synced.ServiceExports == nil || *status.Synced.ServiceExports) &&
		status.Synced.Identities && status.Synced.Endpoints

	return status
}
