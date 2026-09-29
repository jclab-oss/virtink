package apiserver

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	virtv1alpha1 "github.com/smartxworks/virtink/pkg/apis/virt/v1alpha1"
	"github.com/smartxworks/virtink/pkg/apiserver/options"
)

// TestConsole builds the server the way virt-controller does on startup, and
// connects to the console of a running VM.
func TestConsole(t *testing.T) {
	opts := options.NewServerOptions()
	// Serve a generated self-signed cert instead of the mounted one.
	opts.SecureServing.ServerCert.CertKey.CertFile = ""
	opts.SecureServing.ServerCert.CertKey.KeyFile = ""
	// Without a CertDirectory, it's kept in memory instead of written there.
	opts.SecureServing.ServerCert.CertDirectory = ""
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	opts.SecureServing.Listener = listener

	scheme := runtime.NewScheme()
	if err := virtv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	vm := &virtv1alpha1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "vm", Namespace: "default"},
		Status: virtv1alpha1.VirtualMachineStatus{
			Phase:    virtv1alpha1.VirtualMachineRunning,
			NodeName: "node",
		},
	}
	virtClient := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(vm).Build()

	cfg, err := NewConfig(opts, fake.NewSimpleClientset(), virtClient)
	if err != nil {
		t.Fatal(err)
	}
	server, err := cfg.Complete().New()
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/apis/subresources.virt.virtink.smartx.com/v1alpha1/namespaces/default/virtualmachines/vm/console", nil)
	rec := httptest.NewRecorder()
	server.GenericAPIServer.Handler.ServeHTTP(rec, req)

	// No virt-daemon runs on the VM's node, which is looked up once the VM is.
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "no virt-daemon pod found on node node") {
		t.Errorf("got %d %q, want the console proxy to look for virt-daemon", rec.Code, rec.Body.String())
	}
}
