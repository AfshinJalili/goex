package health

import (
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

type Manager struct {
	ready atomic.Bool
}

func NewManager(initialReady bool) *Manager {
	m := &Manager{}
	m.ready.Store(initialReady)
	return m
}

func (m *Manager) SetReady(ready bool) {
	m.ready.Store(ready)
}

func (m *Manager) IsReady() bool {
	return m.ready.Load()
}

func LivenessHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func ReadinessHandler(m *Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if m.IsReady() {
			c.JSON(http.StatusOK, gin.H{"status": "ready"})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
	}
}
