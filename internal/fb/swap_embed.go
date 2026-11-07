//go:build swapembed

package fb

import _ "embed"

//go:embed llama-swap.exe
var embeddedSwap []byte
