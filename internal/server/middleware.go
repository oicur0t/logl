package server

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Create a response writer wrapper to capture status code
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Call the next handler
			next.ServeHTTP(wrapped, r)

			// Log the request
			logger.Info("HTTP request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", wrapped.statusCode),
				zap.Duration("duration", time.Since(start)),
				zap.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

// MTLSMiddleware verifies client certificates
func MTLSMiddleware(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if TLS is used
			if r.TLS == nil {
				logger.Warn("Request without TLS", zap.String("remote_addr", r.RemoteAddr))
				http.Error(w, "TLS required", http.StatusForbidden)
				return
			}

			// Check if client certificate is present
			if len(r.TLS.PeerCertificates) == 0 {
				logger.Warn("Request without client certificate", zap.String("remote_addr", r.RemoteAddr))
				http.Error(w, "Client certificate required", http.StatusForbidden)
				return
			}

			// Get client certificate
			clientCert := r.TLS.PeerCertificates[0]

			logger.Debug("Client authenticated",
				zap.String("subject", clientCert.Subject.String()),
				zap.String("issuer", clientCert.Issuer.String()),
			)

			// Call the next handler
			next.ServeHTTP(w, r)
		})
	}
}

// RecoveryMiddleware recovers from panics
func RecoveryMiddleware(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("Panic recovered",
						zap.Any("error", err),
						zap.String("path", r.URL.Path),
					)
					http.Error(w, "Internal server error", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// responseWriter is a wrapper around http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
