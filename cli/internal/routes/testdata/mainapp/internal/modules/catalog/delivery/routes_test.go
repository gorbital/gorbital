package delivery

import gb "gorbital.dev/gorbital"

func testRoutes(r *gb.Router) { gb.Get(r, "/v1/test-only", (&handlers{}).get) }
