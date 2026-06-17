package server

import (
	"context"
	"fmt"
	"net/http"
	// Registers pprof handlers on http.DefaultServeMux, served by startPprofServer.
	_ "net/http/pprof"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/superplanehq/superplane/pkg/agents"
	agenttools "github.com/superplanehq/superplane/pkg/agents/agent_tools"
	"github.com/superplanehq/superplane/pkg/agents/anthropic"
	"github.com/superplanehq/superplane/pkg/agents/openai"
	"github.com/superplanehq/superplane/pkg/agents/providerresolver"
	"github.com/superplanehq/superplane/pkg/authorization"
	"github.com/superplanehq/superplane/pkg/config"
	"github.com/superplanehq/superplane/pkg/crypto"
	"github.com/superplanehq/superplane/pkg/git"
	gitprovider "github.com/superplanehq/superplane/pkg/git/provider"
	grpc "github.com/superplanehq/superplane/pkg/grpc"
	agentsActions "github.com/superplanehq/superplane/pkg/grpc/actions/agents"
	"github.com/superplanehq/superplane/pkg/jwt"
	"github.com/superplanehq/superplane/pkg/models"
	"github.com/superplanehq/superplane/pkg/networkpolicy"
	"github.com/superplanehq/superplane/pkg/oidc"
	"github.com/superplanehq/superplane/pkg/public"
	registry "github.com/superplanehq/superplane/pkg/registry"
	"github.com/superplanehq/superplane/pkg/registryimports"
	"github.com/superplanehq/superplane/pkg/services"
	"github.com/superplanehq/superplane/pkg/telemetry"
	"github.com/superplanehq/superplane/pkg/usage"
	"github.com/superplanehq/superplane/pkg/workers"
	"gorm.io/gorm"
)

var _ = registryimports.Loaded

var agentProviderOverride = struct {
	sync.Mutex
	provider agents.Provider
}{}

func SetAgentProviderForTests(provider agents.Provider) func() {
	agentProviderOverride.Lock()
	previous := agentProviderOverride.provider
	agentProviderOverride.provider = provider
	agentProviderOverride.Unlock()

	return func() {
		agentProviderOverride.Lock()
		agentProviderOverride.provider = previous
		agentProviderOverride.Unlock()
	}
}

func getAgentProviderOverride() agents.Provider {
	agentProviderOverride.Lock()
	defer agentProviderOverride.Unlock()
	return agentProviderOverride.provider
}

// buildAgentService wires the agent service over a per-organization Resolver.
// The installation-wide provider (from AGENT_* / ANTHROPIC_* env) becomes the
// fallback an organization without its own configured provider uses; the
// resolver layers per-organization providers on top of it.
//
// Enablement stays installation-global for now: with no override, no env
// provider, and no admin-configured installation provider in the DB, agents are
// disabled installation-wide (a clean "agents not enabled" gRPC response, no
// stream worker). Lifting that to true per-organization-only enablement is a
// follow-up tied to the settings UI.
func buildAgentService(authService authorization.Authorization, encryptor crypto.Encryptor) (agents.Resolver, agentsActions.AgentsService) {
	if provider := getAgentProviderOverride(); provider != nil {
		log.WithField("provider", provider.Name()).Info("Managed agents enabled with provider override")
		resolver := agents.StaticResolver(provider)
		return resolver, agents.NewServiceWithResolver(resolver, authService)
	}

	// The env provider is the resolver's fallback; the resolver also serves an
	// admin-configured OpenAI endpoint from the DB, live. Start the service when
	// either exists. (A from-nothing first configuration still needs one restart
	// to pass this boot gate; edits thereafter apply live.)
	fallback := buildInstallationAgentProvider()
	if fallback == nil && !installationAgentConfiguredInDB() {
		return nil, nil
	}

	resolver := providerresolver.New(fallback, encryptor, openAIToolDefinitions())
	return resolver, agents.NewServiceWithResolver(resolver, authService)
}

// installationAgentConfiguredInDB reports whether an installation admin has
// configured an OpenAI-compatible provider in the database, so the agent service
// starts even with no AGENT_* env set (the resolver then serves it live).
func installationAgentConfiguredInDB() bool {
	md, err := models.GetInstallationMetadata()
	if err != nil {
		return false
	}
	return md.UsesOpenAIAgent()
}

// buildInstallationAgentProvider builds the installation-wide agent provider
// selected by AGENT_PROVIDER, or nil when none is configured.
func buildInstallationAgentProvider() agents.Provider {
	switch config.AgentProvider() {
	case config.ProviderOpenAI:
		return buildOpenAIAgentProvider()
	default:
		return buildAnthropicAgentProvider()
	}
}

func buildAnthropicAgentProvider() agents.Provider {
	cfg := config.LoadAnthropicAgentConfig()
	if !cfg.Enabled() {
		log.Info("Anthropic managed agents disabled: missing ANTHROPIC_* env vars")
		return nil
	}

	if err := anthropic.SyncDefaultAgentPrompt(context.Background(), anthropic.Config{
		APIKey:        cfg.APIKey,
		AgentID:       cfg.AgentID,
		EnvironmentID: cfg.EnvironmentID,
	}); err != nil {
		log.WithError(err).Warn("failed to sync Anthropic managed agent prompt; continuing with provider prompt")
	} else {
		log.Info("Anthropic managed agent prompt synced")
	}

	fileResources, err := anthropic.LoadDefaultSessionResources(context.Background(), anthropic.Config{
		APIKey:        cfg.APIKey,
		AgentID:       cfg.AgentID,
		EnvironmentID: cfg.EnvironmentID,
	})
	if err != nil {
		log.WithError(err).Warn("failed to load Anthropic session resources; continuing without mounted references")
		fileResources = nil
	}

	provider, err := anthropic.New(anthropic.Config{
		APIKey:        cfg.APIKey,
		AgentID:       cfg.AgentID,
		EnvironmentID: cfg.EnvironmentID,
		Resources:     fileResources,
	})
	if err != nil {
		log.WithError(err).Warn("failed to initialise Anthropic managed agents provider")
		return nil
	}

	log.Info("Anthropic managed agents enabled")
	return provider
}

// buildOpenAIAgentProvider wires a generic OpenAI-compatible endpoint (a hosted
// service or a local vLLM/llama.cpp server) as the installation-wide agent
// backend, selected via AGENT_PROVIDER=openai. The provider synthesizes sessions
// and the agent loop client-side (see pkg/agents/openai).
func buildOpenAIAgentProvider() agents.Provider {
	cfg := config.LoadOpenAICompatibleAgentConfig()
	if !cfg.Enabled() {
		log.Info("OpenAI-compatible agent provider disabled: set AGENT_BASE_URL and AGENT_MODEL")
		return nil
	}

	provider, err := openai.New(openai.Config{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
		Tools:   openAIToolDefinitions(),
	})
	if err != nil {
		log.WithError(err).Warn("failed to initialise OpenAI-compatible agent provider")
		return nil
	}

	log.WithField("model", cfg.Model).Info("OpenAI-compatible agent provider enabled")
	return provider
}

// openAIToolDefinitions adapts the registered agent tools to the OpenAI
// provider's neutral tool shape (pkg/agents/openai must not import agent_tools).
func openAIToolDefinitions() []openai.ToolDefinition {
	defs := agenttools.DefaultDefinitions()
	out := make([]openai.ToolDefinition, 0, len(defs))
	for _, d := range defs {
		out = append(out, openai.ToolDefinition{
			Name:        d.Name(),
			Description: d.Description(),
			Parameters:  d.InputSchema().Map(),
		})
	}
	return out
}

func startWorkers(
	encryptor crypto.Encryptor,
	registry *registry.Registry,
	oidcProvider oidc.Provider,
	gitProvider gitprovider.Provider,
	baseURL string,
	authService authorization.Authorization,
	agentResolver agents.Resolver,
) {
	log.Println("Starting Workers")

	rabbitMQURL, err := config.RabbitMQURL()
	if err != nil {
		panic(err)
	}

	if os.Getenv("START_CONSUMERS") == "yes" {
		startEmailConsumers(rabbitMQURL, encryptor, baseURL, authService)
	}

	if os.Getenv("START_WORKFLOW_EVENT_ROUTER") == "yes" || os.Getenv("START_EVENT_ROUTER") == "yes" {
		log.Println("Starting Event Router")

		w := workers.NewEventRouter(rabbitMQURL)
		go w.Start(context.Background())
	}

	if os.Getenv("START_WORKFLOW_NODE_EXECUTOR") == "yes" || os.Getenv("START_NODE_EXECUTOR") == "yes" {
		log.Println("Starting Node Executor")

		webhookBaseURL := getWebhookBaseURL(baseURL)
		w := workers.NewNodeExecutor(encryptor, registry, gitProvider, baseURL, webhookBaseURL, rabbitMQURL, authService)
		go w.Start(context.Background())
	}

	if os.Getenv("START_NODE_REQUEST_WORKER") == "yes" {
		log.Println("Starting Node Request Worker")

		webhookBaseURL := getWebhookBaseURL(baseURL)
		w := workers.NewNodeRequestWorker(encryptor, registry, webhookBaseURL, authService)
		go w.Start(context.Background())
	}

	if os.Getenv("START_APP_INSTALLATION_REQUEST_WORKER") == "yes" || os.Getenv("START_INTEGRATION_REQUEST_WORKER") == "yes" {
		log.Println("Starting Integration Request Worker")

		webhooksBaseURL := getWebhookBaseURL(baseURL)
		w := workers.NewIntegrationRequestWorker(encryptor, registry, oidcProvider, baseURL, webhooksBaseURL)
		go w.Start(context.Background())
	}

	if os.Getenv("START_WORKFLOW_NODE_QUEUE_WORKER") == "yes" || os.Getenv("START_NODE_QUEUE_WORKER") == "yes" {
		log.Println("Starting Node Queue Worker")

		w := workers.NewNodeQueueWorker(registry, gitProvider, rabbitMQURL)
		go w.Start(context.Background())
	}

	// Start Webhook Provisioner when internal API runs so integration webhooks (e.g. GCP On VM Created) get provisioned.
	// Can be disabled by setting START_WEBHOOK_PROVISIONER=no.
	if os.Getenv("START_WEBHOOK_PROVISIONER") != "no" {
		if os.Getenv("START_WEBHOOK_PROVISIONER") == "yes" {
			log.Println("Starting Webhook Provisioner")
		}
		webhookBaseURL := getWebhookBaseURL(baseURL)
		w := workers.NewWebhookProvisioner(webhookBaseURL, encryptor, registry)
		go w.Start(context.Background())
	}

	if os.Getenv("START_WEBHOOK_CLEANUP_WORKER") == "yes" {
		log.Println("Starting Webhook Cleanup Worker")

		w := workers.NewWebhookCleanupWorker(encryptor, registry, baseURL)
		go w.Start(context.Background())
	}

	if os.Getenv("START_INSTALLATION_CLEANUP_WORKER") == "yes" || os.Getenv("START_INTEGRATION_CLEANUP_WORKER") == "yes" {
		log.Println("Starting Integration Cleanup Worker")

		w := workers.NewIntegrationCleanupWorker(registry, encryptor, baseURL)
		go w.Start(context.Background())
	}

	if os.Getenv("START_WORKFLOW_CLEANUP_WORKER") == "yes" || os.Getenv("START_CANVAS_CLEANUP_WORKER") == "yes" {
		log.Println("Starting Canvas Cleanup Worker")

		w := workers.NewCanvasCleanupWorkerWithResolver(gitProvider, agentResolver)
		go w.Start(context.Background())
	}

	if os.Getenv("START_REPOSITORY_PROVISIONER") == "yes" {
		log.Println("Starting Repository Provisioner")
		w := workers.NewRepositoryProvisionerWorker(rabbitMQURL, gitProvider)
		go w.Start(context.Background())
	}

	var workerUsageService usage.Service
	initWorkerUsageService := func() (usage.Service, error) {
		if workerUsageService != nil {
			return workerUsageService, nil
		}

		service, err := usage.NewServiceFromEnv()
		if err != nil {
			return nil, err
		}
		workerUsageService = service
		return workerUsageService, nil
	}
	getRequiredWorkerUsageService := func() usage.Service {
		service, err := initWorkerUsageService()
		if err != nil {
			log.Fatalf("failed to initialize usage service worker dependency: %v", err)
		}
		return service
	}
	getOptionalWorkerUsageService := func() usage.Service {
		service, err := initWorkerUsageService()
		if err != nil {
			log.Printf("usage service unavailable for agent canvas tool: %v", err)
			return nil
		}
		return service
	}

	if os.Getenv("START_ORGANIZATION_CLEANUP_WORKER") == "yes" {
		log.Println("Starting Organization Cleanup Worker")

		w := workers.NewOrganizationCleanupWorkerWithResolver(gitProvider, agentResolver)
		go w.Start(context.Background())
	}

	if agentResolver != nil && os.Getenv("START_AGENT_STREAM_WORKER") != "no" {
		log.Println("Starting Agent Stream Worker")
		agentToolRegistry := agenttools.NewRegistry(agenttools.Dependencies{
			Encryptor:         encryptor,
			ComponentRegistry: registry,
			WebhookBaseURL:    getWebhookBaseURL(baseURL),
			AuthService:       authService,
			UsageService:      getOptionalWorkerUsageService(),
		})
		w := workers.NewAgentStreamWorkerWithResolver(agentResolver, rabbitMQURL, agentToolRegistry)
		go w.Start(context.Background())
	}

	if os.Getenv("START_EVENT_RETENTION_WORKER") == "yes" || os.Getenv("START_USAGE_SYNC_WORKER") == "yes" {
		usageService := getRequiredWorkerUsageService()

		if os.Getenv("START_EVENT_RETENTION_WORKER") == "yes" && usageService.Enabled() {
			log.Println("Starting Event Retention Worker")
			w := workers.NewEventRetentionWorker(usageService)
			go w.Start(context.Background())
		}

		if os.Getenv("START_USAGE_SYNC_WORKER") == "yes" && usageService.Enabled() {
			log.Println("Starting Usage Sync Worker")
			w := workers.NewUsageSyncWorker(rabbitMQURL, usageService)
			go w.Start(context.Background())
		}
	}

}

func startEmailConsumers(rabbitMQURL string, encryptor crypto.Encryptor, baseURL string, authService authorization.Authorization) {
	emailService := services.BuildEmailService(encryptor, services.EmailServiceConfig{
		TemplateDir:       os.Getenv("TEMPLATE_DIR"),
		OwnerSetupEnabled: os.Getenv("OWNER_SETUP_ENABLED") == "yes",
		ResendAPIKey:      os.Getenv("RESEND_API_KEY"),
		FromName:          os.Getenv("EMAIL_FROM_NAME"),
		FromEmail:         os.Getenv("EMAIL_FROM_ADDRESS"),
	})
	if emailService == nil {
		log.Warn("Email Consumers not started - missing required environment variables")
		return
	}

	startEmailConsumersWithService(rabbitMQURL, emailService, baseURL, authService)
}

func startEmailConsumersWithService(rabbitMQURL string, emailService services.EmailService, baseURL string, authService authorization.Authorization) {
	log.Println("Starting Invitation Email Consumer")
	invitationEmailConsumer := workers.NewInvitationEmailConsumer(rabbitMQURL, emailService, baseURL)
	go invitationEmailConsumer.Start()

	log.Println("Starting Notification Email Consumer")
	notificationEmailConsumer := workers.NewNotificationEmailConsumer(rabbitMQURL, emailService, authService)
	go notificationEmailConsumer.Start()

	log.Println("Starting Magic Code Email Consumer")
	magicCodeEmailConsumer := workers.NewMagicCodeEmailConsumer(rabbitMQURL, emailService, baseURL)
	go magicCodeEmailConsumer.Start()
}

func startInternalAPI(
	baseURL, webhooksBaseURL, basePath string,
	encryptor crypto.Encryptor,
	jwtSigner *jwt.Signer,
	authService authorization.Authorization,
	registry *registry.Registry,
	oidcProvider oidc.Provider,
	gitProvider gitprovider.Provider,
	agentService agentsActions.AgentsService,
) {
	log.Println("Starting Internal API")

	grpc.RunServer(
		baseURL,
		webhooksBaseURL,
		basePath,
		encryptor,
		jwtSigner,
		authService,
		registry,
		oidcProvider,
		gitProvider,
		agentService,
		lookupInternalAPIPort(),
	)
}

func startPublicAPI(
	baseURL, basePath string,
	encryptor crypto.Encryptor,
	registry *registry.Registry,
	jwtSigner *jwt.Signer,
	oidcProvider oidc.Provider,
	authService authorization.Authorization,
	gitProvider gitprovider.Provider,
) {
	log.Println("Starting Public API with integrated Web Server")

	appEnv := os.Getenv("APP_ENV")
	templateDir := os.Getenv("TEMPLATE_DIR")
	blockSignup := os.Getenv("BLOCK_SIGNUP") == "yes"
	usageService, err := usage.NewServiceFromEnv()
	if err != nil {
		log.Panicf("failed to initialize usage service for public api: %v", err)
	}

	webhooksBaseURL := getWebhookBaseURL(baseURL)
	server, err := public.NewServer(
		encryptor,
		registry,
		jwtSigner,
		oidcProvider,
		gitProvider,
		basePath,
		baseURL,
		webhooksBaseURL,
		appEnv,
		templateDir,
		authService,
		usageService,
		blockSignup,
	)
	if err != nil {
		log.Panicf("Error creating public API server: %v", err)
	}

	// Start the EventDistributer worker if enabled
	if os.Getenv("START_EVENT_DISTRIBUTER") == "yes" {
		log.Println("Starting Event Distributer Worker")
		eventDistributer := workers.NewEventDistributer(server.WebsocketHub())
		go eventDistributer.Start()
	} else {
		log.Println("Event Distributer not started (START_EVENT_DISTRIBUTER != yes)")
	}

	if os.Getenv("START_GRPC_GATEWAY") == "yes" {
		log.Println("Adding gRPC Gateway to Public API")

		grpcServerAddr := os.Getenv("GRPC_SERVER_ADDR")
		if grpcServerAddr == "" {
			grpcServerAddr = "localhost:50051"
		}

		err := server.RegisterGRPCGateway(grpcServerAddr)
		if err != nil {
			log.Fatalf("Failed to register gRPC gateway: %v", err)
		}

		server.RegisterOpenAPIHandler()
	}

	// Register web routes only if START_WEB_SERVER is set to "yes"
	if os.Getenv("START_WEB_SERVER") == "yes" {
		webBasePath := os.Getenv("WEB_BASE_PATH")
		log.Printf("Registering web routes in public API server with base path: %s", webBasePath)
		server.RegisterWebRoutes(webBasePath)
	} else {
		log.Println("Web server routes not registered (START_WEB_SERVER != yes)")
	}

	err = server.Serve("0.0.0.0", lookupPublicAPIPort())
	if err != nil {
		log.Fatal(err)
	}
}

func lookupPublicAPIPort() int {
	port := 8000

	if p := os.Getenv("PUBLIC_API_PORT"); p != "" {
		if v, errConv := strconv.Atoi(p); errConv == nil && v > 0 {
			port = v
		} else {
			log.Warnf("Invalid PUBLIC_API_PORT %q, falling back to 8000", p)
		}
	}

	return port
}

func lookupInternalAPIPort() int {
	port := 50051

	if p := os.Getenv("INTERNAL_API_PORT"); p != "" {
		if v, errConv := strconv.Atoi(p); errConv == nil && v > 0 {
			port = v
		} else {
			log.Warnf("Invalid INTERNAL_API_PORT %q, falling back to 50051", p)
		}
	}

	return port
}

func configureLogging() {
	appEnv := os.Getenv("APP_ENV")

	if appEnv == "development" || appEnv == "test" {
		log.SetFormatter(&log.TextFormatter{
			FullTimestamp:   false,
			TimestampFormat: time.Stamp,
		})
	} else {
		log.SetFormatter(&log.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.StampMilli,
		})
	}
}

func setupOtel() {
	if os.Getenv("OTEL_ENABLED") != "yes" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := telemetry.InitMetrics(ctx); err != nil {
		log.Warnf("Failed to initialize OpenTelemetry metrics: %v", err)
	} else {
		log.Info("OpenTelemetry metrics initialized")
	}

	if err := telemetry.InitTracing(ctx); err != nil {
		log.Warnf("Failed to initialize OpenTelemetry tracing: %v", err)
	} else {
		log.Info("OpenTelemetry tracing initialized for critical API endpoints")
	}
}

func startPprofServer() {
	if os.Getenv("PPROF_ENABLED") != "yes" {
		return
	}

	port := os.Getenv("PPROF_PORT")
	if port == "" {
		port = "6060"
	}

	// Sample contention so /debug/pprof/block and /debug/pprof/mutex are useful.
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(5)

	go func() {
		log.Infof("pprof server listening on :%s", port)
		if err := http.ListenAndServe("0.0.0.0:"+port, nil); err != nil {
			log.Warnf("pprof server stopped: %v", err)
		}
	}()
}

func Start() {
	configureLogging()
	setupOtel()
	startPprofServer()

	telemetry.InitSentry()
	telemetry.StartBeacon()

	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		panic("ENCRYPTION_KEY can't be empty")
	}

	log.SetLevel(log.DebugLevel)

	var encryptorInstance crypto.Encryptor
	if os.Getenv("NO_ENCRYPTION") == "yes" {
		log.Warn("NO_ENCRYPTION is set to yes, using NoOpEncryptor")
		encryptorInstance = crypto.NewNoOpEncryptor()
	} else {
		encryptorInstance = crypto.NewAESGCMEncryptor([]byte(encryptionKey))
	}

	authService, err := authorization.NewAuthService()
	if err != nil {
		log.Fatalf("failed to create auth service: %v", err)
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		panic("BASE_URL must be set")
	}

	basePath := os.Getenv("PUBLIC_API_BASE_PATH")
	if basePath == "" {
		panic("PUBLIC_API_BASE_PATH must be set")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		panic("JWT_SECRET must be set")
	}

	oidcKeysPath := os.Getenv("OIDC_KEYS_PATH")
	if oidcKeysPath == "" {
		panic("OIDC_KEYS_PATH must be set")
	}

	appEnv := os.Getenv("APP_ENV")
	jwtSigner := jwt.NewSigner(jwtSecret)
	webhooksBaseURL := getWebhookBaseURL(baseURL)
	oidcProvider, err := oidc.NewProviderFromKeyDir(webhooksBaseURL, oidcKeysPath)
	if err != nil {
		panic(fmt.Sprintf("failed to load OIDC keys: %v", err))
	}

	log.Println("Creating Git Provider")
	gitProvider, err := git.NewProvider()
	if err != nil {
		panic(fmt.Sprintf("failed to create git provider: %v", err))
	}

	registry, err := registry.NewRegistryWithOptions(registry.RegistryOptions{
		Encryptor: encryptorInstance,
		AppEnv:    appEnv,
		HTTP: registry.HTTPOptions{
			MaxResponseBytes: DefaultMaxHTTPResponseBytes,
			PolicyResolver: func() (registry.HTTPPolicy, error) {
				policy, err := networkpolicy.ResolveHTTPPolicy()
				if err != nil {
					return registry.HTTPPolicy{}, err
				}

				return registry.HTTPPolicy{
					BlockedHosts:    policy.BlockedHosts,
					PrivateIPRanges: policy.PrivateIPRanges,
				}, nil
			},
			PolicyResolverInTransaction: func(tx *gorm.DB) (registry.HTTPPolicy, error) {
				policy, err := networkpolicy.ResolveHTTPPolicyInTransaction(tx)
				if err != nil {
					return registry.HTTPPolicy{}, err
				}

				return registry.HTTPPolicy{
					BlockedHosts:    policy.BlockedHosts,
					PrivateIPRanges: policy.PrivateIPRanges,
				}, nil
			},
			PolicyCacheTTL: 5 * time.Second,
		},
	})

	if err != nil {
		panic(fmt.Sprintf("failed to create registry: %v", err))
	}

	agentResolver, agentService := buildAgentService(authService, encryptorInstance)

	if os.Getenv("START_PUBLIC_API") == "yes" {
		go startPublicAPI(
			baseURL,
			basePath,
			encryptorInstance,
			registry,
			jwtSigner,
			oidcProvider,
			authService,
			gitProvider,
		)
	}

	if os.Getenv("START_INTERNAL_API") == "yes" {
		go startInternalAPI(
			baseURL,
			webhooksBaseURL,
			basePath,
			encryptorInstance,
			jwtSigner,
			authService,
			registry,
			oidcProvider,
			gitProvider,
			agentService,
		)
	}

	startWorkers(
		encryptorInstance,
		registry,
		oidcProvider,
		gitProvider,
		baseURL,
		authService,
		agentResolver,
	)

	log.Println("SuperPlane is UP.")

	select {}
}

// getWebhookBaseURL returns the webhook base URL, using the same pattern as SyncContext.
// Use WEBHOOKS_BASE_URL if set, otherwise fall back to baseURL.
// This allows e2e tests to use a fake/mock webhook URL, and local installations to use a different
// URL for webhooks (e.g., a tunnel URL) when the base app is running on localhost.
func getWebhookBaseURL(baseURL string) string {
	webhookBaseURL := os.Getenv("WEBHOOKS_BASE_URL")
	if webhookBaseURL == "" {
		webhookBaseURL = baseURL
	}
	return webhookBaseURL
}

/*
 * 8MB is the default maximum response size for HTTP responses.
 * This prevents component/trigger implementations from using too much memory,
 * and also from emitting large events.
 */
var DefaultMaxHTTPResponseBytes int64 = 8 * 1024 * 1024
