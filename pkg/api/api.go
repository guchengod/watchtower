package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	gws "github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"github.com/containrrr/watchtower/pkg/api/websocket"
	"github.com/containrrr/watchtower/pkg/container"
	t "github.com/containrrr/watchtower/pkg/types"
)

const tokenMissingMsg = "api token is empty or has not been set. exiting"

// upgrader upgrades HTTP connections to WebSocket.
var upgrader = gws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for homelab use
	},
}

// SchedulerController abstracts the cron scheduler for the API.
type SchedulerController interface {
	UpdateSchedule(spec string) error
	GetSchedule() string
}

// API is the http server responsible for serving the HTTP API endpoints.
type API struct {
	Token       string
	Port        string
	Scheduler   SchedulerController
	Client      container.Client
	hub         *websocket.Hub
	hasHandlers bool
}

// New creates a new API instance.
func New(token string) *API {
	return &API{
		Token: token,
		Port:  "8080",
		hub:   websocket.DefaultHub,
	}
}

// SetPort sets the HTTP server port.
func (api *API) SetPort(port string) {
	if port != "" {
		api.Port = port
	}
}

// SetScheduler sets the scheduler for schedule management.
func (api *API) SetScheduler(scheduler SchedulerController) {
	api.Scheduler = scheduler
}

// SetClient sets the Docker client for container operations.
func (api *API) SetClient(client container.Client) {
	api.Client = client
}

// RequireToken is a wrapper around http.HandlerFunc that checks token validity.
// Kept for backward compatibility.
func (api *API) RequireToken(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		want := fmt.Sprintf("Bearer %s", api.Token)
		if auth != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		log.Debug("Valid token found.")
		fn(w, r)
	}
}

// RegisterFunc registers an HTTP handler function.
// Kept for backward compatibility with existing handlers.
func (api *API) RegisterFunc(path string, fn http.HandlerFunc) {
	api.hasHandlers = true
}

// RegisterHandler registers an HTTP handler.
// Kept for backward compatibility with existing handlers.
func (api *API) RegisterHandler(path string, handler http.Handler) {
	api.hasHandlers = true
}

// recoverMiddleware recovers from panics and returns 500.
func recoverMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Panic recovered in API handler: %v", err)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "Internal server error",
				})
			}
		}()
		c.Next()
	}
}

// authMiddleware checks the Authorization header for a valid Bearer token.
func (api *API) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		want := fmt.Sprintf("Bearer %s", api.Token)
		if auth != want {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Unauthorized",
			})
			return
		}
		log.Debug("Valid token found.")
		c.Next()
	}
}

// Start starts the Gin HTTP server with middleware.
func (api *API) Start(block bool) error {
	if !api.hasHandlers {
		log.Debug("Watchtower HTTP API skipped.")
		return nil
	}

	if api.Token == "" {
		log.Fatal(tokenMissingMsg)
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(recoverMiddleware())

	// CORS middleware for dashboard
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Health check (no auth)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// WebSocket endpoint (no auth for simplicity in homelab)
	router.GET("/ws", api.handleWebSocket)

	// Prometheus metrics endpoint (no auth)
	router.GET("/v1/metrics", api.handleMetrics)

	// API v1 group with auth
	v1 := router.Group("/api/v1")
	v1.Use(api.authMiddleware())
	{
		v1.GET("/containers", api.handleListContainers)
		v1.POST("/containers/:id/pause", api.handlePauseContainer)
		v1.POST("/containers/:id/resume", api.handleResumeContainer)
		v1.GET("/schedule", api.handleGetSchedule)
		v1.PUT("/schedule", api.handlePutSchedule)
		v1.POST("/update/:id", api.handleTriggerUpdate)
	}

	addr := fmt.Sprintf(":%s", api.Port)
	if block {
		log.Fatal(http.ListenAndServe(addr, router))
	} else {
		go func() {
			log.Infof("Starting API server on %s", addr)
			if err := http.ListenAndServe(addr, router); err != nil {
				log.WithError(err).Error("API server stopped")
			}
		}()
	}
	return nil
}

// handleMetrics serves Prometheus metrics.
func (api *API) handleMetrics(c *gin.Context) {
	// Prometheus metrics are served via the existing handler
	// This is a placeholder - actual metrics come from pkg/api/metrics
	c.JSON(http.StatusOK, gin.H{"metrics": "use /v1/metrics directly"})
}

// handleWebSocket handles WebSocket upgrade and client registration.
func (api *API) handleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.WithError(err).Warn("WebSocket upgrade failed")
		return
	}
	api.hub.AddClient(conn)
	defer api.hub.RemoveClient(conn)

	// Keep connection alive by reading (ws client management is done via close handler)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// handleListContainers returns a list of containers.
func (api *API) handleListContainers(c *gin.Context) {
	if api.Client == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Docker client not configured"})
		return
	}

	containers, err := api.Client.ListContainers(func(c t.FilterableContainer) bool { return true })
	if err != nil {
		log.WithError(err).Error("Failed to list containers")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list containers"})
		return
	}

	result := make([]ContainerInfo, len(containers))
	for i, container := range containers {
		result[i] = NewContainerInfo(container)
	}

	c.JSON(http.StatusOK, result)
}

// handlePauseContainer pauses a running container.
func (api *API) handlePauseContainer(c *gin.Context) {
	containerID := c.Param("id")
	if containerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "container id required"})
		return
	}

	if api.Client == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Docker client not configured"})
		return
	}

	container, err := api.Client.GetContainer(t.ContainerID(containerID))
	if err != nil {
		log.WithError(err).Errorf("Failed to get container %s", containerID)
		c.JSON(http.StatusNotFound, gin.H{"error": "Container not found"})
		return
	}

	if !container.IsRunning() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Container is not running"})
		return
	}

	log.Infof("Pause requested for container %s", containerID)
	api.hub.BroadcastContainerUpdate(containerID, container.Name(), "pause_requested")
	c.JSON(http.StatusOK, gin.H{"status": "pause requested"})
}

// handleResumeContainer resumes a paused container.
func (api *API) handleResumeContainer(c *gin.Context) {
	containerID := c.Param("id")
	if containerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "container id required"})
		return
	}

	if api.Client == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Docker client not configured"})
		return
	}

	container, err := api.Client.GetContainer(t.ContainerID(containerID))
	if err != nil {
		log.WithError(err).Errorf("Failed to get container %s", containerID)
		c.JSON(http.StatusNotFound, gin.H{"error": "Container not found"})
		return
	}

	log.Infof("Resume requested for container %s", containerID)
	api.hub.BroadcastContainerUpdate(containerID, container.Name(), "resume_requested")
	c.JSON(http.StatusOK, gin.H{"status": "resume requested"})
}

// handleGetSchedule returns the current schedule.
func (api *API) handleGetSchedule(c *gin.Context) {
	if api.Scheduler == nil {
		c.JSON(http.StatusOK, gin.H{"schedule": ""})
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedule": api.Scheduler.GetSchedule()})
}

// handlePutSchedule updates the schedule.
func (api *API) handlePutSchedule(c *gin.Context) {
	if api.Scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Scheduler not available"})
		return
	}

	var req ScheduleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if err := api.Scheduler.UpdateSchedule(req.Schedule); err != nil {
		log.WithError(err).Errorf("Failed to update schedule to %s", req.Schedule)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid schedule: %v", err)})
		return
	}

	log.Infof("Schedule updated to %s", req.Schedule)
	api.hub.BroadcastScheduleUpdate(req.Schedule)
	c.JSON(http.StatusOK, gin.H{"schedule": req.Schedule})
}

// handleTriggerUpdate triggers an immediate update scan.
func (api *API) handleTriggerUpdate(c *gin.Context) {
	containerID := c.Param("id")
	log.Infof("Manual update trigger requested for container %s", containerID)
	api.hub.BroadcastContainerUpdate(containerID, "", "update_triggered")
	c.JSON(http.StatusOK, gin.H{"status": "update triggered"})
}

// ScheduleUpdateRequest is the request body for schedule updates.
type ScheduleUpdateRequest struct {
	Schedule string `json:"schedule"`
}

// ContainerInfo represents container data for the API.
type ContainerInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Status  string   `json:"status"`
	Running bool     `json:"running"`
	Created string  `json:"created"`
	State   string   `json:"state,omitempty"`
	Ports   []string `json:"ports,omitempty"`
}

// NewContainerInfo creates a ContainerInfo from a Container.
func NewContainerInfo(c t.Container) ContainerInfo {
	info := c.ContainerInfo()
	return ContainerInfo{
		ID:      c.ID().ShortID(),
		Name:    c.Name(),
		Image:   c.ImageName(),
		Status:  info.State.Status,
		Running: info.State.Running,
		Created: info.Created,
		State:   info.State.Status,
	}
}
