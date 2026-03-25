package scheduler

import (
	"fmt"
	"sync"

	"github.com/robfig/cron"
	log "github.com/sirupsen/logrus"
)

// Controller allows runtime modification of the cron schedule.
type Controller struct {
	mu         sync.RWMutex
	spec       string
	scheduler  *cron.Cron
	updateLock chan bool
	onUpdate  func()
}

// New creates a new scheduler controller.
func New(spec string, updateLock chan bool) *Controller {
	return &Controller{
		spec:       spec,
		updateLock: updateLock,
		scheduler:  cron.New(),
	}
}

// Start begins the cron scheduler with the configured spec.
func (c *Controller) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.spec == "" {
		log.Debug("No schedule configured, scheduler not started")
		return nil
	}

	err := c.scheduler.AddFunc(c.spec, c.runUpdate)
	if err != nil {
		return fmt.Errorf("failed to add schedule function: %w", err)
	}

	c.scheduler.Start()
	log.Infof("Scheduler started with spec: %s", c.spec)
	return nil
}

// Stop gracefully stops the scheduler.
func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scheduler != nil {
		c.scheduler.Stop()
	}
}

// runUpdate is the function called by the cron scheduler.
func (c *Controller) runUpdate() {
	select {
	case v := <-c.updateLock:
		defer func() { c.updateLock <- v }()
		log.Debug("Scheduled update triggered")
		if c.onUpdate != nil {
			c.onUpdate()
		}
	default:
		log.Debug("Skipped scheduled update - another update already running")
	}
}

// SetOnUpdate sets the callback for scheduled updates.
func (c *Controller) SetOnUpdate(callback func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onUpdate = callback
}

// UpdateSchedule updates the cron schedule at runtime.
func (c *Controller) UpdateSchedule(newSpec string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.scheduler == nil {
		return fmt.Errorf("scheduler not initialized")
	}

	// Stop existing entries
	c.scheduler.Stop()
	c.scheduler = cron.New()

	if newSpec == "" {
		c.spec = ""
		log.Info("Schedule cleared")
		return nil
	}

	// Create a new scheduler with the updated spec
	err := c.scheduler.AddFunc(newSpec, c.runUpdate)
	if err != nil {
		return fmt.Errorf("invalid cron spec: %w", err)
	}

	c.spec = newSpec
	c.scheduler.Start()
	log.Infof("Schedule updated to: %s", newSpec)
	return nil
}

// GetSchedule returns the current cron schedule spec.
func (c *Controller) GetSchedule() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.spec
}
