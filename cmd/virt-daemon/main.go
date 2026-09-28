package main

import (
	"flag"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	virtv1alpha1 "github.com/smartxworks/virtink/pkg/apis/virt/v1alpha1"
	"github.com/smartxworks/virtink/pkg/daemon"
	"github.com/smartxworks/virtink/pkg/daemon/balloon"
	"github.com/smartxworks/virtink/pkg/daemon/deviceplugin"
	"github.com/smartxworks/virtink/pkg/daemon/tcpproxy"
	"github.com/smartxworks/virtink/pkg/rootfscache"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(virtv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var rootfsCacheTTL time.Duration
	var rootfsCacheGCInterval time.Duration
  
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.DurationVar(&rootfsCacheTTL, "rootfs-cache-ttl", 24*time.Hour, "How long a cached imageRootfs disk is kept after it's no longer used by any VM on the node.")
	flag.DurationVar(&rootfsCacheGCInterval, "rootfs-cache-gc-interval", 10*time.Minute, "How often unused cached imageRootfs disks are looked for.")
	
  balloonConfig := balloon.DefaultConfig()
	var balloonInterval time.Duration
	flag.DurationVar(&balloonInterval, "balloon-interval", 10*time.Second, "The interval to adjust memory balloons of VMs. 0 disables automatic ballooning.")
	flag.Float64Var(&balloonConfig.LowWatermark, "balloon-low-watermark", balloonConfig.LowWatermark, "Inflate balloons when the ratio of node available memory drops below this value.")
	flag.Float64Var(&balloonConfig.HighWatermark, "balloon-high-watermark", balloonConfig.HighWatermark, "Deflate balloons when the ratio of node available memory rises above this value.")
	flag.Float64Var(&balloonConfig.PSIThreshold, "balloon-psi-threshold", balloonConfig.PSIThreshold, "Inflate balloons when the node memory PSI some avg10 (%) exceeds this value.")
	flag.Float64Var(&balloonConfig.StepRatio, "balloon-step-ratio", balloonConfig.StepRatio, "The ratio of (maxSize - minSize) to resize balloons by per interval.")
	
  opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	if err = (&daemon.VMReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		Recorder:      mgr.GetEventRecorderFor("virt-daemon"),
		NodeName:      os.Getenv("NODE_NAME"),
		NodeIP:        os.Getenv("NODE_IP"),
		RelayProvider: tcpproxy.NewRelayProvider(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VM")
		os.Exit(1)
	}

	if balloonInterval > 0 {
		if err = mgr.Add(&daemon.BalloonManager{
			Client:   mgr.GetClient(),
			NodeName: os.Getenv("NODE_NAME"),
			Config:   balloonConfig,
			Interval: balloonInterval,
			ProcPath: "/proc",
		}); err != nil {
			setupLog.Error(err, "unable to create balloon manager")
			os.Exit(1)
		}
	}

	if err = mgr.Add(deviceplugin.NewDevicePluginManager()); err != nil {
		setupLog.Error(err, "unable to create device plugin manager")
		os.Exit(1)
	}

	if err = mgr.Add(&rootfscache.GarbageCollector{
		Cache:    rootfscache.Cache{Dir: rootfscache.DefaultDir},
		PodsDir:  "/var/lib/kubelet/pods",
		TTL:      rootfsCacheTTL,
		Interval: rootfsCacheGCInterval,
		Log:      ctrl.Log.WithName("rootfs-cache-gc"),
	}); err != nil {
		setupLog.Error(err, "unable to create rootfs cache garbage collector")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
