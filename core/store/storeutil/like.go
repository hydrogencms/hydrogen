package storeutil

import "strings"

// LikeContains turns s into a LIKE pattern (with ESCAPE '\') matching values that contain it.
func LikeContains(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s) + "%"
}
