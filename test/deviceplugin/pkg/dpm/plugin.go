package dpm

import (
	"net"
	"os"
	"path"
	"sync"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// PluginInterface is a mandatory interface that must be implemented by all plugins. It is
// identical to DevicePluginServer interface of device plugin API. In version v1alpha this
// interface contains methods Allocate and ListAndWatch. For more information see
// https://godoc.org/k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1alpha#DevicePluginServer
type PluginInterface interface {
	pluginapi.DevicePluginServer
}

// PluginInterfaceStart is an optional interface that could be implemented by plugin. If case Start
// is implemented, it will be executed by Manager after plugin instantiation and before its
// registrartion to kubelet. This method could be used to prepare resources before they are offered
// to Kubernetes.
type PluginInterfaceStart interface {
	Start() error
}

// PluginInterfaceStop is an optional interface that could be implemented by plugin. If case Stop
// is implemented, it will be executed by Manager after the plugin is unregistered from kubelet.
// This method could be used to tear down resources.
type PluginInterfaceStop interface {
	Stop() error
}

// DevicePlugin represents a gRPC server client/server.
type devicePlugin struct {
	DevicePluginImpl PluginInterface
	ResourceName     string
	Name             string
	Socket           string
	Server           *grpc.Server
	Running          bool
	Starting         *sync.Mutex
}

func newDevicePlugin(resourceNamespace string, pluginName string, devicePluginImpl PluginInterface) devicePlugin {
	return devicePlugin{
		DevicePluginImpl: devicePluginImpl,
		Socket:           pluginapi.DevicePluginPath + resourceNamespace + "_" + pluginName,
		ResourceName:     resourceNamespace + "/" + pluginName,
		Name:             pluginName,
		Starting:         &sync.Mutex{},
	}
}

// StartServer starts the gRPC server and registers the device plugin to Kubelet. Calling
// StartServer on started object is NOOP.
func (dpi *devicePlugin) StartServer() error {
	klog.V(3).InfoS("Starting plugin server", "plugin", dpi.Name)

	// If Kubelet socket is created, we may try to start the same plugin concurrently. To avoid
	// that, let's make plugins startup a critical section.
	dpi.Starting.Lock()
	defer dpi.Starting.Unlock()

	// If we've acquired the lock after waiting for the Start to finish, we don't need to do
	// anything (as long as the plugin is running).
	if dpi.Running {
		return nil
	}

	err := dpi.serve()
	if err != nil {
		return err
	}

	err = dpi.register()
	if err != nil {
		dpi.StopServer() //nolint:errcheck
		return err
	}
	dpi.Running = true

	return nil
}

// serve starts the gRPC server of the device plugin.
func (dpi *devicePlugin) serve() error {
	klog.V(3).InfoS("Starting the DPI gRPC server", "plugin", dpi.Name)

	err := dpi.cleanup()
	if err != nil {
		klog.ErrorS(err, "Failed to setup a DPI gRPC server", "plugin", dpi.Name)
		return err
	}

	sock, err := net.Listen("unix", dpi.Socket)
	if err != nil {
		klog.ErrorS(err, "Failed to setup a DPI gRPC server", "plugin", dpi.Name)
		return err
	}

	dpi.Server = grpc.NewServer([]grpc.ServerOption{}...)
	pluginapi.RegisterDevicePluginServer(dpi.Server, dpi.DevicePluginImpl)

	go dpi.Server.Serve(sock) //nolint:errcheck
	klog.V(3).InfoS("Serving requests...", "plugin", dpi.Name)
	// Wait till grpc server is ready.
	for i := 0; i < 10; i++ {
		services := dpi.Server.GetServiceInfo()
		if len(services) > 1 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	return nil
}

// register registers the device plugin (as gRPC client call) for the given ResourceName with
// Kubelet DPI infrastructure.
func (dpi *devicePlugin) register() error {
	klog.V(3).InfoS("Registering the DPI with Kubelet", "plugin", dpi.Name)

	conn, err := grpc.Dial(pluginapi.KubeletSocket, grpc.WithInsecure(),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}))
	defer conn.Close() //nolint:errcheck
	if err != nil {
		klog.ErrorS(err, "Could not dial gRPC", "plugin", dpi.Name)
		return err
	}
	client := pluginapi.NewRegistrationClient(conn)
	klog.InfoS("Registration for endpoint", "plugin", dpi.Name, "endpoint", path.Base(dpi.Socket))

	options, err := dpi.DevicePluginImpl.GetDevicePluginOptions(context.Background(), &pluginapi.Empty{})
	if err != nil {
		klog.ErrorS(err, "Failed to get device plugin options", "plugin", dpi.Name)
		return err
	}

	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(dpi.Socket),
		ResourceName: dpi.ResourceName,
		Options:      options,
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		klog.ErrorS(err, "Registration failed", "plugin", dpi.Name)
		klog.ErrorS(err, "Make sure that the DevicePlugins feature gate is enabled and kubelet running", "plugin", dpi.Name)
		return err
	}
	return nil
}

// StopServer stops the gRPC server. Trying to stop already stopped plugin emits an info-level
// log message.
func (dpi *devicePlugin) StopServer() error {
	// TODO: should this also be a critical section?
	// how do we prevent multiple stops? or start/stop race condition?
	klog.V(3).InfoS("Stopping plugin server", "plugin", dpi.Name)

	if !dpi.Running {
		klog.V(3).InfoS("Tried to stop stopped DPI", "plugin", dpi.Name)
		return nil
	}

	klog.V(3).InfoS("Stopping the DPI gRPC server", "plugin", dpi.Name)
	dpi.Server.Stop()
	dpi.Running = false

	return dpi.cleanup()
}

// cleanup is a helper to remove DPI's socket.
func (dpi *devicePlugin) cleanup() error {
	if err := os.Remove(dpi.Socket); err != nil && !os.IsNotExist(err) {
		klog.ErrorS(err, "Could not clean up socket", "plugin", dpi.Name, "socket", dpi.Socket)
		return err
	}

	return nil
}
