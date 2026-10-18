//go:build cgo

package syntax

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>

typedef const void *(*lang_fn)(void);

static const void *call_lang_fn(void *fn) {
	return ((lang_fn)fn)();
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	grammarMu    sync.Mutex
	grammarCache = map[string]*tree_sitter.Language{}
)

// loadGrammar opens runtime/grammars/<name>.so (or .dylib) and returns
// the tree-sitter language. Without such a file it asks embedded, when
// set, for a library path. Results are cached for the process.
func loadGrammar(runtimeDirs []string, name string, embedded EmbeddedGrammars) (*tree_sitter.Language, error) {
	grammarMu.Lock()
	defer grammarMu.Unlock()

	if lang, ok := grammarCache[name]; ok {
		return lang, nil
	}

	path := ""
	for _, dir := range runtimeDirs {
		for _, ext := range []string{".so", ".dylib"} {
			p := filepath.Join(dir, "grammars", name+ext)
			if _, err := os.Stat(p); err == nil {
				path = p

				break
			}
		}

		if path != "" {
			break
		}
	}

	if path == "" && embedded != nil {
		p, err := embedded(name)
		if err != nil && !errors.Is(err, ErrGrammarNotEmbedded) {
			return nil, err
		}

		path = p
	}

	if path == "" {
		return nil, fmt.Errorf("grammar %q not found in runtime", name)
	}

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	handle := C.dlopen(cPath, C.RTLD_NOW|C.RTLD_LOCAL)
	if handle == nil {
		return nil, fmt.Errorf("load grammar %q: %s", name, C.GoString(C.dlerror()))
	}

	symbol := "tree_sitter_" + strings.ReplaceAll(name, "-", "_")
	cSym := C.CString(symbol)
	defer C.free(unsafe.Pointer(cSym))
	fn := C.dlsym(handle, cSym)
	if fn == nil {
		C.dlclose(handle)

		return nil, fmt.Errorf("grammar %q has no symbol %s", name, symbol)
	}

	lang := tree_sitter.NewLanguage(C.call_lang_fn(fn))
	grammarCache[name] = lang

	return lang, nil
}
