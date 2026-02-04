package trace

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/gin-gonic/gin"
)

func Middleware(serviceName string) gin.HandlerFunc {
	tracer := otel.Tracer(serviceName)
	prop := otel.GetTextMapPropagator()
	if prop == nil {
		prop = propagation.TraceContext{}
		otel.SetTextMapPropagator(prop)
	}

	return func(c *gin.Context) {
		ctx := prop.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		ctx, span := tracer.Start(ctx, c.FullPath())
		defer span.End()

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
