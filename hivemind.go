package hivemind

import _ "embed"

//go:embed src/hooks/hivemind_hooks.py
var HivemindHooksPy []byte

//go:embed src/hooks/mock_file_emitter.py
var MockFileEmitterPy []byte

