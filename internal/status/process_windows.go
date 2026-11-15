// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package status

// ProcessExists is not probed on Windows: freshness of the file decides.
func ProcessExists(pid int) bool { return pid > 0 }
