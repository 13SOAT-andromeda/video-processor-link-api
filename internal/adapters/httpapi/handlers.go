package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fiap/links-service/internal/app"
	"github.com/fiap/links-service/internal/domain/link"
)

type Handlers struct {
	svc *app.Service
}

func NewHandlers(svc *app.Service) *Handlers { return &Handlers{svc: svc} }

// Router monta as rotas conforme o contrato da spec §5.
func Router(svc *app.Service, jwtSecret string) *gin.Engine {
	h := NewHandlers(svc)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	auth := r.Group("/", AuthMiddleware(jwtSecret))
	{
		auth.POST("/links", h.CreateLink)
		auth.GET("/links", RequireRole("administrator"), h.ListAll)
		auth.GET("/links/user/:id", h.ListByUser)
		auth.GET("/links/:id", h.GetLink)
		auth.GET("/links/:id/events", h.ListEvents)
		auth.PUT("/links/:id/upload", h.ConfirmUpload)
		auth.GET("/links/:id/download", h.Download)
	}
	return r
}

type createLinkRequest struct {
	FileName  string `json:"fileName" binding:"required"`
	FileSize  int64  `json:"fileSize"`
	IsPrivate bool   `json:"isPrivate"`
}

func (h *Handlers) CreateLink(c *gin.Context) {
	var req createLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_REQUEST", "detail": err.Error()})
		return
	}
	if req.FileSize > link.MaxFileSizeBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_FILE_SIZE"})
		return
	}
	out, err := h.svc.CreateLink(c.Request.Context(), c.GetString("userId"), req.FileName, req.IsPrivate)
	if err != nil {
		abortWithError(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

func (h *Handlers) ListAll(c *gin.Context) {
	links, err := h.svc.ListAll(c.Request.Context())
	if err != nil {
		abortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, links)
}

func (h *Handlers) ListByUser(c *gin.Context) {
	links, err := h.svc.ListByUser(c.Request.Context(), c.Param("id"), c.GetString("userId"), c.GetString("role"))
	if err != nil {
		abortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, links)
}

func (h *Handlers) GetLink(c *gin.Context) {
	l, err := h.svc.GetLink(c.Request.Context(), c.Param("id"), c.GetString("userId"), c.GetString("role"))
	if err != nil {
		abortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, l)
}

func (h *Handlers) ListEvents(c *gin.Context) {
	events, err := h.svc.ListEvents(c.Request.Context(), c.Param("id"), c.GetString("userId"), c.GetString("role"))
	if err != nil {
		abortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, events)
}

func (h *Handlers) ConfirmUpload(c *gin.Context) {
	l, err := h.svc.ConfirmUpload(c.Request.Context(), c.Param("id"), c.GetString("userId"), c.GetString("role"))
	if err != nil {
		abortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, l)
}

func (h *Handlers) Download(c *gin.Context) {
	out, err := h.svc.Download(c.Request.Context(), c.Param("id"), c.GetString("userId"), c.GetString("role"))
	if err != nil {
		abortWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// abortWithError mapeia erros de domínio para os códigos da spec §5.
func abortWithError(c *gin.Context, err error) {
	var invalid link.ErrInvalidTransition
	switch {
	case errors.Is(err, link.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "LINK_NOT_FOUND"})
	case errors.Is(err, link.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "FORBIDDEN"})
	case errors.As(err, &invalid):
		c.JSON(http.StatusConflict, gin.H{"error": "INVALID_STATUS_TRANSITION", "detail": invalid.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL_ERROR"})
	}
}
