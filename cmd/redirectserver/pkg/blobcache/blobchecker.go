// Copyright 2022 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package blobcache

import (
	"net/http"
	"sync"

	"k8s.io/klog/v2"
)

// BlobChecker are used to check if a blob exists, possibly with caching
type BlobChecker interface {
	// BlobExists should check that blobURL exists
	// bucket and layerHash may be used for caching purposes
	BlobExists(blobURL, bucket, layerHash string) bool
}

// cachedBlobChecker just performs an HTTP HEAD check against the blob
//
// TODO: potentially replace with a caching implementation
// should be plenty fast for now, HTTP HEAD on s3 is cheap
type cachedBlobChecker struct {
	httpClient *http.Client
	blobCache
}

func NewCachedBlobChecker(httpClient *http.Client) BlobChecker {
	return &cachedBlobChecker{
		httpClient: httpClient,
		blobCache: blobCache{
			cache: make(map[string]map[string]struct{}),
		},
	}
}

type blobCache struct {
	// cache contains bucket:key for observed keys
	// it is not bounded, we can afford to store all keys if need be
	// and the cloud run container will spin down after an idle period
	cache map[string]map[string]struct{}
	lock  sync.RWMutex
}

func (b *blobCache) Get(bucket, layerHash string) bool {
	b.lock.RLock()
	defer b.lock.RUnlock()
	if m, exists := b.cache[bucket]; exists {
		_, exists = m[layerHash]
		return exists
	}
	return false
}

func (b *blobCache) Put(bucket, layerHash string) {
	b.lock.Lock()
	defer b.lock.Unlock()
	if _, exists := b.cache[bucket]; !exists {
		b.cache[bucket] = make(map[string]struct{})
	}
	b.cache[bucket][layerHash] = struct{}{}
}

func (c *cachedBlobChecker) BlobExists(blobURL, bucket, layerHash string) bool {
	if c.blobCache.Get(bucket, layerHash) {
		klog.V(3).InfoS("blob existence cache hit", "url", blobURL)
		return true
	}
	klog.V(3).InfoS("blob existence cache miss", "url", blobURL)
	r, err := c.httpClient.Head(blobURL)
	// fallback to assuming blob is unavailable on errors
	if err != nil {
		return false
	}
	r.Body.Close()
	// if the blob exists it HEAD should return 200 OK
	// this is true for S3 and for OCI registries
	if r.StatusCode == http.StatusOK {
		c.blobCache.Put(bucket, layerHash)
		return true
	}
	return false
}
