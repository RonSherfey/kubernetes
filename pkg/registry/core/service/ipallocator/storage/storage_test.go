/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package storage

import (
	"context"
	"net"
	"strings"
	"testing"

	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/storage"
	etcd3testing "k8s.io/apiserver/pkg/storage/etcd3/testing"
	"k8s.io/apiserver/pkg/storage/storagebackend/factory"
	api "k8s.io/kubernetes/pkg/apis/core"
	_ "k8s.io/kubernetes/pkg/apis/core/install"
	"k8s.io/kubernetes/pkg/registry/core/service/allocator"
	allocatorstore "k8s.io/kubernetes/pkg/registry/core/service/allocator/storage"
	"k8s.io/kubernetes/pkg/registry/core/service/ipallocator"
	"k8s.io/kubernetes/pkg/registry/registrytest"
)

func newStorage(t *testing.T) (*etcd3testing.EtcdTestServer, ipallocator.Interface, allocator.Interface, storage.Interface, factory.DestroyFunc) {
	etcdStorage, server := registrytest.NewEtcdStorage(t, "")
	_, cidr, err := net.ParseCIDR("192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}

	var backing allocator.Interface
	storage, err := ipallocator.New(cidr, func(max int, rangeSpec string) (allocator.Interface, error) {
		mem := allocator.NewAllocationMap(max, rangeSpec)
		backing = mem
		etcd, err := allocatorstore.NewEtcd(mem, "/ranges/serviceips", api.Resource("serviceipallocations"), etcdStorage)
		if err != nil {
			return nil, err
		}
		return etcd, nil
	})
	if err != nil {
		t.Fatalf("unexpected error creating etcd: %v", err)
	}
	s, d, err := generic.NewRawStorage(etcdStorage, nil)
	if err != nil {
		t.Fatalf("Couldn't create storage: %v", err)
	}
	destroyFunc := func() {
		d()
		server.Terminate(t)
	}
	return server, storage, backing, s, destroyFunc
}

func validNewRangeAllocation() *api.RangeAllocation {
	_, cidr, _ := net.ParseCIDR("192.168.1.0/24")
	return &api.RangeAllocation{
		Range: cidr.String(),
	}
}

func key() string {
	return "/ranges/serviceips"
}

func TestEmpty(t *testing.T) {
	_, storage, _, _, destroyFunc := newStorage(t)
	defer destroyFunc()
	if err := storage.Allocate(net.ParseIP("192.168.1.2")); !strings.Contains(err.Error(), "cannot allocate resources of type serviceipallocations at this time") {
		t.Fatal(err)
	}
}

func TestErrors(t *testing.T) {
	_, storage, _, _, destroyFunc := newStorage(t)
	defer destroyFunc()
	err := storage.Allocate(net.ParseIP("192.168.0.0"))
	if _, ok := err.(*ipallocator.ErrNotInRange); !ok {
		t.Fatal(err)
	}
}

func TestStore(t *testing.T) {
	_, storage, backing, si, destroyFunc := newStorage(t)
	defer destroyFunc()
	if err := si.Create(context.TODO(), key(), validNewRangeAllocation(), nil, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := storage.Allocate(net.ParseIP("192.168.1.2")); err != nil {
		t.Fatal(err)
	}
	ok, err := backing.Allocate(1)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("Expected allocation to fail")
	}
	if err := storage.Allocate(net.ParseIP("192.168.1.2")); err != ipallocator.ErrAllocated {
		t.Fatal(err)
	}
}
