package backend

import "testing"

func TestBuildFreeTurnArgsEnablesRoutes(t *testing.T) {
	args := buildFreeTurnArgs(ConnectParams{}, &ProfileData{PeerAddr: "127.0.0.1:19302"}, freeTurnModeTCP)
	found := false
	for _, arg := range args {
		if arg == "-routes" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("buildFreeTurnArgs() = %v, want -routes", args)
	}
}

func TestTrackTurnRouteKeepsRouteThroughRemovalLog(t *testing.T) {
	e := &FreeturnEngine{turnRoutes: make(map[string]struct{})}

	e.trackTurnRoute("2026/09/16 12:00:00 Ensuring route to 193.203.43.16/32 via 10.0.0.1")
	if _, ok := e.turnRoutes["193.203.43.16"]; !ok {
		t.Fatal("TURN route was not tracked")
	}

	// FreeTurn logs this before executing route deletion. Keep the route tracked
	// until process exit so fallback cleanup can verify whether deletion actually
	// succeeded.
	e.trackTurnRoute("2026/09/16 12:00:01 Removing route to 193.203.43.16/32")
	if _, ok := e.turnRoutes["193.203.43.16"]; !ok {
		t.Fatal("TURN route was forgotten before deletion could be verified")
	}
}

func TestTrackTurnRouteForgetsFailedAdd(t *testing.T) {
	e := &FreeturnEngine{turnRoutes: make(map[string]struct{})}

	e.trackTurnRoute("Ensuring route to 91.231.135.171/32 via 10.0.0.1")
	if _, ok := e.turnRoutes["91.231.135.171"]; !ok {
		t.Fatal("TURN route was not tracked")
	}

	e.trackTurnRoute("failed to add route to 91.231.135.171: exit status 1")
	if _, ok := e.turnRoutes["91.231.135.171"]; ok {
		t.Fatal("failed route add remained tracked; fallback cleanup could delete a route it did not create")
	}
}

func TestTrackTurnRouteIgnoresInvalidAddress(t *testing.T) {
	e := &FreeturnEngine{turnRoutes: make(map[string]struct{})}
	e.trackTurnRoute("Ensuring route to 999.999.999.999/32 via 10.0.0.1")
	if len(e.turnRoutes) != 0 {
		t.Fatalf("invalid route was tracked: %v", e.turnRoutes)
	}
}
