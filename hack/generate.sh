#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

bash $GOPATH/src/k8s.io/code-generator/generate-groups.sh "deepcopy,client,informer,lister" \
  github.com/smartxworks/virtink/pkg/generated github.com/smartxworks/virtink/pkg/apis \
  virt:v1alpha1 \
  --go-header-file ./hack/boilerplate.go.txt

bash $GOPATH/src/k8s.io/code-generator/generate-groups.sh "deepcopy" \
  github.com/smartxworks/virtink/pkg/generated github.com/smartxworks/virtink/pkg/apis \
  subresources:v1alpha1 \
  --go-header-file ./hack/boilerplate.go.txt

# The aggregated API server needs the OpenAPI models of the types it serves.
go run k8s.io/kube-openapi/cmd/openapi-gen --go-header-file ./hack/boilerplate.go.txt \
  --output-dir pkg/generated/openapi --output-pkg github.com/smartxworks/virtink/pkg/generated/openapi \
  --report-filename /dev/null \
  ./pkg/apis/subresources/v1alpha1 k8s.io/apimachinery/pkg/apis/meta/v1 k8s.io/apimachinery/pkg/runtime k8s.io/apimachinery/pkg/version

controller-gen paths=./pkg/apis/... crd output:crd:artifacts:config=deploy/crd
controller-gen paths=./cmd/virt-controller/... paths=./pkg/controller/... rbac:roleName=virt-controller output:rbac:artifacts:config=deploy/virt-controller webhook output:webhook:artifacts:config=deploy/virt-controller
controller-gen paths=./cmd/virt-daemon/... paths=./pkg/daemon/... rbac:roleName=virt-daemon output:rbac:artifacts:config=deploy/virt-daemon

go generate ./...
