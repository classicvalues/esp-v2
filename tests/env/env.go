// Copyright 2019 Google LLC
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

package env

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/esp-v2/tests/env/components"
	"github.com/GoogleCloudPlatform/esp-v2/tests/env/platform"
	"github.com/GoogleCloudPlatform/esp-v2/tests/env/testdata"
	"github.com/golang/glog"

	bookserver "github.com/GoogleCloudPlatform/esp-v2/tests/endpoints/bookstore_grpc/server"
	annotationspb "google.golang.org/genproto/googleapis/api/annotations"
	confpb "google.golang.org/genproto/googleapis/api/serviceconfig"
)

const (
	// Additional wait time after `TestEnv.Setup`
	setupWaitTime = 1 * time.Second
	initRolloutId = "test-rollout-id"
)

var (
	debugComponents = flag.String("debug_components", "", `display debug logs for components, can be "all", "envoy", "configmanager", "bootstrap"`)
)

type TestEnv struct {
	backend platform.Backend

	mockMetadata                    bool
	enableScNetworkFailOpen         bool
	enableEchoServerRootPathHandler bool
	mockMetadataOverride            map[string]string
	mockMetadataFailures            int
	mockIamResps                    map[string]string
	mockIamFailures                 int
	mockIamRespTime                 time.Duration
	bookstoreServer                 *bookserver.BookstoreServer
	grpcInteropServer               *components.GrpcInteropGrpcServer
	grpcEchoServer                  *components.GrpcEchoGrpcServer
	configMgr                       *components.ConfigManagerServer
	echoBackend                     *components.EchoHTTPServer
	envoy                           *components.Envoy
	rolloutId                       string
	fakeServiceConfig               *confpb.Service
	MockMetadataServer              *components.MockMetadataServer
	MockIamServer                   *components.MockIamServer
	backendAuthIamServiceAccount    string
	backendAuthIamDelegates         string
	serviceControlIamServiceAccount string
	serviceControlIamDelegates      string
	MockServiceManagementServer     *components.MockServiceMrg
	backendAddress                  string
	ports                           *platform.Ports
	envoyDrainTimeInSec             int
	ServiceControlServer            *components.MockServiceCtrl
	FakeStackdriverServer           *components.FakeTraceServer
	enableTracing                   bool
	tracingSampleRate               float32
	healthRegistry                  *components.HealthRegistry
	FakeJwtService                  *components.FakeJwtService
	skipHealthChecks                bool
	skipEnvoyHealthChecks           bool
	StatsVerifier                   *components.StatsVerifier

	// Only implemented for a subset of backends.
	backendMTLSCertFile         string
	useWrongBackendCert         bool
	backendAlwaysRespondRST     bool
	backendNotStart             bool
	backendRejectRequestNum     int
	backendRejectRequestStatus  int
	disableHttp2ForHttpsBackend bool
}

func NewTestEnv(testId uint16, backend platform.Backend) *TestEnv {
	glog.Infof("Running test function #%v", testId)

	fakeServiceConfig := testdata.SetupServiceConfig(backend)

	return &TestEnv{
		backend:                     backend,
		mockMetadata:                true,
		MockServiceManagementServer: components.NewMockServiceMrg(fakeServiceConfig.GetName(), initRolloutId, fakeServiceConfig),
		ports:                       platform.NewPorts(testId),
		rolloutId:                   initRolloutId,
		fakeServiceConfig:           fakeServiceConfig,
		ServiceControlServer:        components.NewMockServiceCtrl(fakeServiceConfig.GetName(), initRolloutId),
		healthRegistry:              components.NewHealthRegistry(),
		FakeJwtService:              components.NewFakeJwtService(),
		FakeStackdriverServer:       components.NewFakeStackdriver(),
	}
}

// SetEnvoyDrainTimeInSec
func (e *TestEnv) SetEnvoyDrainTimeInSec(envoyDrainTimeInSec int) {
	e.envoyDrainTimeInSec = envoyDrainTimeInSec
}

// OverrideMockMetadata overrides mock metadata values given path to response map.
func (e *TestEnv) OverrideMockMetadata(newImdsData map[string]string, imdsFailures int) {
	e.mockMetadataOverride = newImdsData
	e.mockMetadataFailures = imdsFailures
}

func (e *TestEnv) SetBackendAddress(backendAddress string) {
	e.backendAddress = backendAddress
}

// Dictates the responses and the number of failures mock IAM will respond with.
func (e *TestEnv) SetIamResps(iamResps map[string]string, iamFailures int, iamRespTime time.Duration) {
	e.mockIamResps = iamResps
	e.mockIamFailures = iamFailures
	e.mockIamRespTime = iamRespTime
}

func (e *TestEnv) SetBackendAuthIamServiceAccount(serviecAccount string) {
	e.backendAuthIamServiceAccount = serviecAccount
}

func (e *TestEnv) SetBackendAuthIamDelegates(delegates string) {
	e.backendAuthIamDelegates = delegates
}

func (e *TestEnv) SetServiceControlIamServiceAccount(serviecAccount string) {
	e.serviceControlIamServiceAccount = serviecAccount
}

func (e *TestEnv) SetServiceControlIamDelegates(delegates string) {
	e.serviceControlIamDelegates = delegates
}

// OverrideBackend overrides the mock backend only.
// Warning: This will result in using the service config for the original backend,
// even though the new backend is spun up.
func (e *TestEnv) OverrideBackendService(backend platform.Backend) {
	e.backend = backend
}

// For use when dynamic routing is enabled.
// By default, it uses same cert as Envoy for HTTPS calls. When useWrongBackendCert
// is set to true, purposely fail HTTPS calls for testing.
func (e *TestEnv) UseWrongBackendCertForDR(useWrongBackendCert bool) {
	e.useWrongBackendCert = useWrongBackendCert
}

func (e *TestEnv) SetBackendAlwaysRespondRST(backendAlwaysRespondRST bool) {
	e.backendAlwaysRespondRST = backendAlwaysRespondRST
}

func (e *TestEnv) SetBackendNotStart(backendNotStart bool) {
	e.backendNotStart = backendNotStart
}

func (e *TestEnv) SetBackendRejectRequestNum(backendFaRequestNum int) {
	e.backendRejectRequestNum = backendFaRequestNum
}

func (e *TestEnv) SetBackendRejectRequestStatus(backendFaRequestStatus int) {
	e.backendRejectRequestStatus = backendFaRequestStatus
}

// SetBackendMTLSCert sets the backend cert file to enable mutual authentication.
func (e *TestEnv) SetBackendMTLSCert(fileName string) {
	e.backendMTLSCertFile = fileName
}

// Ports returns test environment ports.
func (e *TestEnv) Ports() *platform.Ports {
	return e.ports
}

// OverrideAuthentication overrides Service.Authentication.
func (e *TestEnv) OverrideAuthentication(authentication *confpb.Authentication) {
	e.fakeServiceConfig.Authentication = authentication
}

// OverrideAuthentication overrides Service.Authentication.
func (e *TestEnv) OverrideRolloutIdAndConfigId(newRolloutId, newConfigId string) {
	e.fakeServiceConfig.Id = newConfigId
	e.rolloutId = newRolloutId
	e.MockServiceManagementServer.SetRolloutId(newRolloutId)
	e.ServiceControlServer.SetRolloutIdConfigIdInReport(newRolloutId)
}

func (e *TestEnv) ServiceConfigId() string {
	if e.fakeServiceConfig == nil {
		return ""
	}
	return e.fakeServiceConfig.Id
}

// OverrideSystemParameters overrides Service.SystemParameters.
func (e *TestEnv) OverrideSystemParameters(systemParameters *confpb.SystemParameters) {
	e.fakeServiceConfig.SystemParameters = systemParameters
}

// OverrideQuota overrides Service.Quota.
func (e *TestEnv) OverrideQuota(quota *confpb.Quota) {
	e.fakeServiceConfig.Quota = quota
}

// AppendHttpRules appends Service.Http.Rules.
func (e *TestEnv) AppendHttpRules(rules []*annotationspb.HttpRule) {
	e.fakeServiceConfig.Http.Rules = append(e.fakeServiceConfig.Http.Rules, rules...)
}

// AppendBackendRules appends Service.Backend.Rules.
func (e *TestEnv) AppendBackendRules(rules []*confpb.BackendRule) {
	if e.fakeServiceConfig.Backend == nil {
		e.fakeServiceConfig.Backend = &confpb.Backend{}
	}
	e.fakeServiceConfig.Backend.Rules = append(e.fakeServiceConfig.Backend.Rules, rules...)
}

// RemoveAllBackendRules removes all Service.Backend.Rules.
// This is useful for testing
func (e *TestEnv) RemoveAllBackendRules() {
	e.fakeServiceConfig.Backend = &confpb.Backend{}
}

// EnableScNetworkFailOpen sets enableScNetworkFailOpen to be true.
func (e *TestEnv) EnableScNetworkFailOpen() {
	e.enableScNetworkFailOpen = true
}

// AppendUsageRules appends Service.Usage.Rules.
func (e *TestEnv) AppendUsageRules(rules []*confpb.UsageRule) {
	e.fakeServiceConfig.Usage.Rules = append(e.fakeServiceConfig.Usage.Rules, rules...)
}

// SetAllowCors Sets AllowCors in API endpoint to true.
func (e *TestEnv) SetAllowCors() {
	e.fakeServiceConfig.Endpoints[0].AllowCors = true
}

func (e *TestEnv) EnableEchoServerRootPathHandler() {
	e.enableEchoServerRootPathHandler = true
}

// Limit usage of this, as it causes flakes in CI.
// Only intended to be used to test if Envoy starts up correctly.
// Ideally, the test using this should have it's own retry loop.
// Can also call after setup but before teardown to skip teardown checks.
func (e *TestEnv) SkipHealthChecks() {
	e.skipHealthChecks = true
}

// SkipEnvoyHealthChecks skips health check on Envoy listener port. But not on admin port
// Keeping health check on Envoy admin port will help to prevent test flakyness.
func (e *TestEnv) SkipEnvoyHealthChecks() {
	e.skipEnvoyHealthChecks = true
}

// In the service config for each backend, the backend port is represented with 2 constants.
// Replace them as needed.
func addDynamicRoutingBackendPort(serviceConfig *confpb.Service, port uint16) error {
	for _, rule := range serviceConfig.Backend.GetRules() {
		if rule.Address == "" {
			// Empty address is now allowed, ESPv2 will override with `--backend` flag.
			continue
		}

		if !strings.Contains(rule.Address, platform.WorkingBackendPort) && !strings.Contains(rule.Address, platform.InvalidBackendPort) {
			return fmt.Errorf("backend rule address (%v) is not properly formatted", rule.Address)
		}

		rule.Address = strings.ReplaceAll(rule.Address, platform.WorkingBackendPort, strconv.Itoa(int(port)))
	}
	return nil
}

func (e *TestEnv) SetupFakeTraceServer(sampleRate float32) {
	e.enableTracing = true
	e.tracingSampleRate = sampleRate
}

func (e *TestEnv) DisableHttp2ForHttpsBackend() {
	e.disableHttp2ForHttpsBackend = true
}

// Setup setups Envoy, Config Manager, and Backend server for test.
func (e *TestEnv) Setup(confArgs []string) error {
	var envoyArgs []string
	var bootstrapperArgs []string
	mockJwtProviders := make(map[string]bool)
	if e.MockServiceManagementServer != nil {
		if err := addDynamicRoutingBackendPort(e.fakeServiceConfig, e.ports.DynamicRoutingBackendPort); err != nil {
			return err
		}

		for _, rule := range e.fakeServiceConfig.GetAuthentication().GetRules() {
			for _, req := range rule.GetRequirements() {
				if providerId := req.GetProviderId(); providerId != "" {
					mockJwtProviders[providerId] = true
				}
			}
		}

		glog.Infof("Requested JWT providers for this test: %v", mockJwtProviders)
		if err := e.FakeJwtService.SetupJwt(mockJwtProviders, e.ports); err != nil {
			return err
		}

		for providerId := range mockJwtProviders {
			provider, ok := e.FakeJwtService.ProviderMap[providerId]
			if !ok {
				return fmt.Errorf("not supported jwt provider id: %v", providerId)
			}
			auth := e.fakeServiceConfig.GetAuthentication()
			auth.Providers = append(auth.Providers, provider.AuthProvider)
		}

		e.ServiceControlServer.Setup()
		testdata.SetFakeControlEnvironment(e.fakeServiceConfig, e.ServiceControlServer.GetURL())
		confArgs = append(confArgs, "--service_control_url="+e.ServiceControlServer.GetURL())
		if err := testdata.AppendLogMetrics(e.fakeServiceConfig); err != nil {
			return err
		}

		confArgs = append(confArgs, "--service_management_url="+e.MockServiceManagementServer.Start())
	}

	if !e.enableScNetworkFailOpen {
		confArgs = append(confArgs, "--service_control_network_fail_open=false")
	}

	if e.mockMetadata {
		e.MockMetadataServer = components.NewMockMetadata(e.mockMetadataOverride, e.mockMetadataFailures)
		confArgs = append(confArgs, "--metadata_url="+e.MockMetadataServer.GetURL())
		bootstrapperArgs = append(bootstrapperArgs, "--metadata_url="+e.MockMetadataServer.GetURL())
	}

	if e.mockIamResps != nil || e.mockIamFailures != 0 || e.mockIamRespTime != 0 {
		e.MockIamServer = components.NewIamMetadata(e.mockIamResps, e.mockIamFailures, e.mockIamRespTime)
		confArgs = append(confArgs, "--iam_url="+e.MockIamServer.GetURL())
	}

	if e.backendAuthIamServiceAccount != "" {
		confArgs = append(confArgs, "--backend_auth_iam_service_account="+e.backendAuthIamServiceAccount)
	}

	if e.backendAuthIamDelegates != "" {
		confArgs = append(confArgs, "--backend_auth_iam_delegates="+e.backendAuthIamDelegates)
	}

	if e.serviceControlIamServiceAccount != "" {
		confArgs = append(confArgs, "--service_control_iam_service_account="+e.serviceControlIamServiceAccount)
	}

	if e.serviceControlIamDelegates != "" {
		confArgs = append(confArgs, "--service_control_iam_delegates="+e.serviceControlIamDelegates)
	}

	confArgs = append(confArgs, fmt.Sprintf("--listener_port=%v", e.ports.ListenerPort))
	confArgs = append(confArgs, fmt.Sprintf("--service=%v", e.fakeServiceConfig.Name))

	// Tracing configuration.
	if e.enableTracing {
		confArgs = append(confArgs, fmt.Sprintf("--tracing_sample_rate=%v", e.tracingSampleRate))
		// This address must be in gRPC format: https://github.com/grpc/grpc/blob/master/doc/naming.md
		confArgs = append(confArgs, fmt.Sprintf("--tracing_stackdriver_address=%v:%v:%v", platform.GetIpProtocol(), platform.GetLoopbackAddress(), e.ports.FakeStackdriverPort))
	} else {
		confArgs = append(confArgs, "--disable_tracing")
	}

	// Starts XDS.
	var err error
	debugConfigMgr := *debugComponents == "all" || *debugComponents == "configmanager"

	if *debugComponents == "all" || *debugComponents == "bootstrap" {
		bootstrapperArgs = append(bootstrapperArgs, "--logtostderr", "--v=1")
	}

	// Set backend flag (for sidecar)
	if e.backendAddress == "" {
		backendAddress, err := formBackendAddress(e.ports, e.backend)
		if err != nil {
			return fmt.Errorf("unable to form backend address: %v", err)
		}
		e.backendAddress = backendAddress
	}

	if e.backendAddress != "" {
		confArgs = append(confArgs, "--backend_address", e.backendAddress)
	}

	e.configMgr, err = components.NewConfigManagerServer(debugConfigMgr, e.ports, confArgs)
	if err != nil {
		return err
	}
	if err = e.configMgr.StartAndWait(); err != nil {
		return err
	}
	e.healthRegistry.RegisterHealthChecker(e.configMgr)

	// Starts envoy.
	envoyConfPath := fmt.Sprintf("/tmp/apiproxy-testdata-bootstrap-%v.yaml", e.ports.TestId)
	if *debugComponents == "all" || *debugComponents == "envoy" {
		envoyArgs = append(envoyArgs, "--log-level", "debug")
		if e.envoyDrainTimeInSec == 0 {
			envoyArgs = append(envoyArgs, "--drain-time-s", "1")
		}
	}
	if e.envoyDrainTimeInSec != 0 {
		envoyArgs = append(envoyArgs, "--drain-time-s", strconv.Itoa(e.envoyDrainTimeInSec))
	}

	e.envoy, err = components.NewEnvoy(envoyArgs, bootstrapperArgs, envoyConfPath, e.ports)
	if err != nil {
		glog.Errorf("unable to create Envoy %v", err)
		return err
	}
	if !e.skipEnvoyHealthChecks {
		e.healthRegistry.RegisterHealthChecker(e.envoy)
	}

	if err = e.envoy.StartAndWait(); err != nil {
		return err
	}

	e.StatsVerifier = components.NewStatsVerifier(e.ports)
	e.healthRegistry.RegisterHealthChecker(e.StatsVerifier)
	e.FakeStackdriverServer.StartStackdriverServer(e.ports.FakeStackdriverPort)

	if !e.backendNotStart {
		switch e.backend {
		case platform.EchoSidecar:
			e.echoBackend, err = components.NewEchoHTTPServer(e.ports.BackendServerPort /*useWrongCert*/, false, &components.EchoHTTPServerFlags{
				EnableHttps:                false,
				EnableRootPathHandler:      e.enableEchoServerRootPathHandler,
				MtlsCertFile:               e.backendMTLSCertFile,
				DisableHttp2:               e.disableHttp2ForHttpsBackend,
				BackendAlwaysRespondRST:    e.backendAlwaysRespondRST,
				BackendRejectRequestNum:    e.backendRejectRequestNum,
				BackendRejectRequestStatus: e.backendRejectRequestStatus,
			})

			if err != nil {
				return err
			}
			if err := e.echoBackend.StartAndWait(); err != nil {
				return err
			}
		case platform.EchoRemote:
			e.echoBackend, err = components.NewEchoHTTPServer(e.ports.DynamicRoutingBackendPort /*useWrongCert*/, e.useWrongBackendCert, &components.EchoHTTPServerFlags{
				EnableHttps:                true,
				EnableRootPathHandler:      true,
				MtlsCertFile:               e.backendMTLSCertFile,
				DisableHttp2:               e.disableHttp2ForHttpsBackend,
				BackendAlwaysRespondRST:    e.backendAlwaysRespondRST,
				BackendRejectRequestNum:    e.backendRejectRequestNum,
				BackendRejectRequestStatus: e.backendRejectRequestStatus,
			})
			if err != nil {
				return err
			}
			if err := e.echoBackend.StartAndWait(); err != nil {
				return err
			}
		case platform.GrpcBookstoreSidecar:
			e.bookstoreServer, err = bookserver.NewBookstoreServer(e.ports.BackendServerPort /*enableTLS=*/, false /*useAuthorizedBackendCert*/, false /*backendMTLSCertFile=*/, "")
			if err != nil {
				return err
			}
			e.bookstoreServer.StartServer()
		case platform.GrpcBookstoreRemote:
			e.bookstoreServer, err = bookserver.NewBookstoreServer(e.ports.DynamicRoutingBackendPort /*enableTLS=*/, true, e.useWrongBackendCert, e.backendMTLSCertFile)
			if err != nil {
				return err
			}
			e.bookstoreServer.StartServer()
		case platform.GrpcInteropSidecar:
			e.grpcInteropServer, err = components.NewGrpcInteropGrpcServer(e.ports.BackendServerPort)
			if err != nil {
				return err
			}
			if err := e.grpcInteropServer.StartAndWait(); err != nil {
				return err
			}
		case platform.GrpcEchoSidecar:
			e.grpcEchoServer, err = components.NewGrpcEchoGrpcServer(e.ports.BackendServerPort)
			if err != nil {
				return err
			}
			if err := e.grpcEchoServer.StartAndWait(); err != nil {
				return err
			}
		case platform.GrpcEchoRemote:
			e.grpcEchoServer, err = components.NewGrpcEchoGrpcServer(e.ports.DynamicRoutingBackendPort)
			if err != nil {
				return err
			}
			if err := e.grpcEchoServer.StartAndWait(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("backend (%v) is not supported", e.backend)
		}
	}

	time.Sleep(setupWaitTime)

	// Run health checks
	if !e.skipHealthChecks {
		if err := e.healthRegistry.RunAllHealthChecks(); err != nil {
			return err
		}
	}

	return nil
}

func (e *TestEnv) StopBackendServer() error {
	var retErr error
	// Only one backend is instantiated for test.
	if e.echoBackend != nil {
		if err := e.echoBackend.StopAndWait(); err != nil {
			retErr = err
		}
		e.echoBackend = nil
	}
	if e.bookstoreServer != nil {
		e.bookstoreServer.StopServer()
		e.bookstoreServer = nil
	}
	return retErr
}

// TearDown shutdown the servers.
func (e *TestEnv) TearDown(t *testing.T) {
	glog.Infof("start tearing down...")

	// Run all health checks. If they fail, our test causes a server to crash.
	// Fail the test.
	if !e.skipHealthChecks {
		if err := e.healthRegistry.RunAllHealthChecks(); err != nil {
			t.Errorf("health check failure during teardown: %v", err)
		}
	}

	// Verify invariants in statistics.
	if e.StatsVerifier != nil {
		if err := e.StatsVerifier.VerifyInvariants(); err != nil {
			t.Errorf("Error verifying stats invariants: %v", err)
		}
	}

	// Verify invariants in tracing.
	if e.FakeStackdriverServer != nil {
		if err := e.FakeStackdriverServer.VerifyInvariants(); err != nil {
			t.Errorf("Error verifying tracing invariants: %v", err)
		}
	}

	// Tear down servers.
	if e.FakeJwtService != nil {
		e.FakeJwtService.TearDown()
	}

	if e.configMgr != nil {
		if err := e.configMgr.StopAndWait(); err != nil {
			glog.Errorf("error stopping config manager: %v", err)
		}
	}

	if e.envoy != nil {
		if err := e.envoy.StopAndWait(); err != nil {
			glog.Errorf("error stopping envoy: %v", err)
		}
	}

	if e.echoBackend != nil {
		if err := e.echoBackend.StopAndWait(); err != nil {
			glog.Errorf("error stopping Echo Server: %v", err)
		}
	}
	if e.bookstoreServer != nil {
		e.bookstoreServer.StopServer()
		e.bookstoreServer = nil
	}
	if e.grpcInteropServer != nil {
		if err := e.grpcInteropServer.StopAndWait(); err != nil {
			glog.Errorf("error stopping GrpcInterop Server: %v", err)
		}
	}
	if e.grpcEchoServer != nil {
		if err := e.grpcEchoServer.StopAndWait(); err != nil {
			glog.Errorf("error stopping GrpcEcho Server: %v", err)
		}
	}

	e.FakeStackdriverServer.StopAndWait()

	glog.Infof("finish tearing down...")
}

// Form the backend address.
func formBackendAddress(ports *platform.Ports, backend platform.Backend) (string, error) {

	backendAddress := fmt.Sprintf("%v:%v", platform.GetLoopbackHost(), ports.BackendServerPort)

	switch backend {
	case platform.GrpcEchoRemote, platform.EchoRemote, platform.GrpcBookstoreRemote:
		// Dynamic routing backends shouldn't have this flag set.
		return "", nil
	case platform.GrpcBookstoreSidecar, platform.GrpcEchoSidecar, platform.GrpcInteropSidecar:
		return fmt.Sprintf("grpc://%v", backendAddress), nil
	case platform.EchoSidecar:
		return fmt.Sprintf("http://%v", backendAddress), nil
	default:
		return "", fmt.Errorf("backend (%v) is not supported", backend)
	}
}
