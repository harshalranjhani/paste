package app

import "net/http"

const robotsTxt = "User-agent: *\nDisallow: /\n"

func (s *Server) handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(robotsTxt))
}
