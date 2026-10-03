//go:build !windows

package backend

func uiManagesTurnRoutes() bool { return false }

func addManagedTurnRoute(_ string) (*managedTurnRoute, error) { return nil, nil }

func deleteManagedTurnRoute(_ string, _ managedTurnRoute) error { return nil }

func verifyManagedTurnRoutes(_ []string) error { return nil }
