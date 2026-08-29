// Command snowflake is a CLI and tiny HTTP server for the lock-free Snowflake
// ID generator.
//
//	snowflake generate [N]          print N IDs (default 1,000,000)
//	snowflake bench    [N]          measure raw generation throughput
//	snowflake decode   <ID>         split an ID into timestamp/machine/sequence
//	snowflake serve    [--addr]     serve IDs over HTTP
//
// The headline demo:
//
//	time ./snowflake generate 1000000
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ysinghc/snowflake-lite/snowflake"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "generate":
		gen, rest := setup("generate", os.Args[2:])
		cmdGenerate(gen, parseCount(rest, 1_000_000))
	case "bench":
		gen, rest := setup("bench", os.Args[2:])
		cmdBench(gen, parseCount(rest, 1_000_000))
	case "decode":
		fs := flag.NewFlagSet("decode", flag.ExitOnError)
		_ = fs.Parse(os.Args[2:])
		if fs.NArg() < 1 {
			fatal(errors.New("usage: snowflake decode <id>"))
		}
		cmdDecode(fs.Arg(0))
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		machine := fs.Int64("machine", -1, "machine ID (0-1023)")
		addr := fs.String("addr", ":8080", "listen address")
		_ = fs.Parse(os.Args[2:])
		gen, err := newGen(*machine)
		if err != nil {
			fatal(err)
		}
		cmdServe(gen, *addr)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

// setup builds a per-command flag set carrying --machine and returns the
// generator plus any positional arguments.
func setup(name string, args []string) (*snowflake.Generator, []string) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	machine := fs.Int64("machine", -1, "machine ID (0-1023); default $MACHINE_ID or hostname hash")
	_ = fs.Parse(args)
	gen, err := newGen(*machine)
	if err != nil {
		fatal(err)
	}
	return gen, fs.Args()
}

func newGen(machine int64) (*snowflake.Generator, error) {
	if machine < 0 {
		return snowflake.NewFromEnv()
	}
	return snowflake.New(machine)
}

func parseCount(args []string, def int64) int64 {
	if len(args) == 0 {
		return def
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(args[0], "_", ""), 10, 64)
	if err != nil || n < 0 {
		fatal(fmt.Errorf("invalid count %q", args[0]))
	}
	return n
}

// cmdGenerate prints n IDs, one per line, through a large buffered writer so
// the `time ...` demo measures generation rather than syscall overhead.
func cmdGenerate(gen *snowflake.Generator, n int64) {
	w := bufio.NewWriterSize(os.Stdout, 1<<20)
	defer w.Flush()
	buf := make([]byte, 0, 24)
	for i := int64(0); i < n; i++ {
		buf = strconv.AppendInt(buf[:0], int64(gen.Next()), 10)
		buf = append(buf, '\n')
		_, _ = w.Write(buf)
	}
}

// cmdBench measures pure generation throughput and proves the two headline
// properties cheaply: a strictly increasing sequence is, by definition, both
// sortable and collision-free.
func cmdBench(gen *snowflake.Generator, n int64) {
	prev := snowflake.ID(-1)
	monotonic := true
	start := time.Now()
	for i := int64(0); i < n; i++ {
		id := gen.Next()
		if id <= prev {
			monotonic = false
		}
		prev = id
	}
	elapsed := time.Since(start)
	rate := float64(n) / elapsed.Seconds()

	p := snowflake.Decode(prev)
	fmt.Printf("generated : %s IDs\n", commas(n))
	fmt.Printf("elapsed   : %s\n", elapsed.Round(time.Microsecond))
	fmt.Printf("rate      : %s IDs/sec\n", commas(int64(rate)))
	fmt.Printf("sortable  : %v  (strictly ascending ⇒ also collision-free)\n", monotonic)
	fmt.Printf("machine   : %d\n", gen.MachineID())
	fmt.Printf("last ID   : %d\n", prev)
	fmt.Printf("            → %s  seq=%d\n", p.Timestamp.Format(time.RFC3339Nano), p.Sequence)
}

func cmdDecode(arg string) {
	raw, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		fatal(fmt.Errorf("invalid ID %q: %w", arg, err))
	}
	p := snowflake.Decode(snowflake.ID(raw))
	fmt.Printf("ID        : %d\n", raw)
	fmt.Printf("timestamp : %s  (%d ms since 2024-01-01)\n", p.Timestamp.Format(time.RFC3339Nano), p.MillisSinceEpoch)
	fmt.Printf("machine   : %d\n", p.MachineID)
	fmt.Printf("sequence  : %d\n", p.Sequence)
	fmt.Printf("bits      : %s\n", bitString(raw))
	fmt.Printf("            %s\n", bitLegend())
}

// cmdServe exposes the generator over HTTP. IDs are returned as JSON strings on
// purpose: a Snowflake exceeds 2^53 and would be corrupted by a JSON number.
func cmdServe(gen *snowflake.Generator, addr string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/id", func(w http.ResponseWriter, r *http.Request) {
		n := 1
		if q := r.URL.Query().Get("n"); q != "" {
			if v, err := strconv.Atoi(q); err == nil {
				n = v
			}
		}
		if n < 1 {
			n = 1
		}
		if n > 10_000 {
			n = 10_000
		}
		writeJSON(w)
		if n == 1 {
			_ = json.NewEncoder(w).Encode(map[string]string{"id": gen.Next().String()})
			return
		}
		ids := make([]string, n)
		for i := range ids {
			ids[i] = gen.Next().String()
		}
		_ = json.NewEncoder(w).Encode(map[string][]string{"ids": ids})
	})

	mux.HandleFunc("/decode", func(w http.ResponseWriter, r *http.Request) {
		raw, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid or missing id"}`, http.StatusBadRequest)
			return
		}
		p := snowflake.Decode(snowflake.ID(raw))
		writeJSON(w)
		_ = json.NewEncoder(w).Encode(struct {
			ID               string `json:"id"`
			Timestamp        string `json:"timestamp"`
			MillisSinceEpoch uint64 `json:"millisSinceEpoch"`
			MachineID        uint64 `json:"machineID"`
			Sequence         uint64 `json:"sequence"`
		}{strconv.FormatInt(raw, 10), p.Timestamp.Format(time.RFC3339Nano), p.MillisSinceEpoch, p.MachineID, p.Sequence})
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "snowflake-lite  (machine %d)\n\n", gen.MachineID())
		fmt.Fprint(w, "GET /id            one ID\nGET /id?n=100      up to 10000 IDs\nGET /decode?id=..  decode an ID\nGET /healthz       liveness\n")
	})

	fmt.Printf("snowflake-lite serving on %s  (machine %d)\n", addr, gen.MachineID())
	if err := http.ListenAndServe(addr, cors(mux)); err != nil {
		fatal(err)
	}
}

// cors allows the endpoint to be called from any static page (e.g. a demo site).
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter) { w.Header().Set("Content-Type", "application/json") }

// bitString renders the 64-bit value grouped by field:
// [unused] [41 timestamp] [10 machine] [12 sequence].
func bitString(v int64) string {
	b := fmt.Sprintf("%064b", uint64(v))
	return b[0:1] + " " + b[1:42] + " " + b[42:52] + " " + b[52:64]
}

func bitLegend() string {
	return "^unused " + strings.Repeat(" ", 40) + "timestamp  " + "  machine   " + "  sequence"
}

func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out strings.Builder
	if pre := len(s) % 3; pre > 0 {
		out.WriteString(s[:pre])
		if len(s) > pre {
			out.WriteByte(',')
		}
	}
	for i := len(s) % 3; i < len(s); i += 3 {
		out.WriteString(s[i : i+3])
		if i+3 < len(s) {
			out.WriteByte(',')
		}
	}
	if neg {
		return "-" + out.String()
	}
	return out.String()
}

func usage() {
	fmt.Fprint(os.Stderr, `snowflake — distributed unique ID generator (Twitter Snowflake, lock-free)

Usage:
  snowflake generate [N]   print N IDs, one per line   (default 1,000,000)
  snowflake bench    [N]   measure generation throughput
  snowflake decode   <ID>  split an ID into its fields
  snowflake serve          serve IDs over HTTP (--addr :8080)

Flags (place before the count):
  --machine ID   machine ID 0-1023; default $MACHINE_ID, else hostname hash
  --addr   ADDR  listen address for serve (default :8080)

Examples:
  time ./snowflake generate 1000000
  ./snowflake bench 5000000
  ./snowflake --machine 7 generate 1000000   # (via: snowflake generate --machine 7 1000000)
  ./snowflake decode 123456789012345678
`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
