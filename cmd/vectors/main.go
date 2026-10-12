// vectors writes the shared rules test vectors to testdata/vectors (or -out).
// See testdata/vectors/README.md.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/cribbager/cribbager/internal/vectors"
)

func main() {
	out := flag.String("out", filepath.Join("testdata", "vectors"), "output directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	for name, data := range vectors.Generate() {
		if err := os.WriteFile(filepath.Join(*out, name), data, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("wrote %s (%d bytes)\n", filepath.Join(*out, name), len(data))
	}
}
