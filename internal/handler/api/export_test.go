package api

import "github.com/go-chi/chi/v5"

// withTestRoutes mounts routes into the authenticated group of s -- the chain runs before them. Only
// a test can reach it: the shipped surface has no route a test invented (the brief's "üretime boş
// uç ekleme").
func withTestRoutes(s *Surface, f func(r chi.Router)) { s.testRoutes = f }
