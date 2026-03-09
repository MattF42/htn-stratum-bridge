package htnstratum

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// WorkerLiveStat holds a point-in-time snapshot of live stats for one worker.
type WorkerLiveStat struct {
	Name            string
	LiveGHs         float64
	OneHrGHs        float64
	TwentyFourHrGHs float64
	BlocksFound     int64
}

// getLiveWorkerStats returns a sorted snapshot of all workers currently in the
// shareHandler's stats map.  Returns nil when sh is nil.
func getLiveWorkerStats(sh *shareHandler, addr string) []WorkerLiveStat {
	if sh == nil {
		return nil
	}
	sh.statsLock.Lock()
	defer sh.statsLock.Unlock()

	out := make([]WorkerLiveStat, 0, len(sh.stats))
	for _, v := range sh.stats {
		if v.WalletAddr == addr {
		out = append(out, WorkerLiveStat{
			Name:            v.WorkerName,
			LiveGHs:         GetAverageHashrateGHs(v),
			OneHrGHs:        GetOneHourAverageHashrateGHs(v),
			TwentyFourHrGHs: GetRollingAverageHashrateGHs(v),
			BlocksFound:     v.BlocksFound.Load(),
		})
	}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// indexTmpl is the wallet-address lookup form shown at GET /.
var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>HTN Stratum Bridge – Miner Stats</title>
<style>
  body{font-family:sans-serif;max-width:900px;margin:40px auto;padding:0 16px;background:#1a1a2e;color:#eee}
  h1{color:#00d4ff}
  input{padding:8px 12px;width:100%;box-sizing:border-box;background:#16213e;border:1px solid #0f3460;color:#eee;border-radius:4px;font-size:14px}
  button{margin-top:10px;padding:10px 24px;background:#0f3460;color:#eee;border:none;border-radius:4px;cursor:pointer;font-size:14px}
  button:hover{background:#00d4ff;color:#1a1a2e}
  .error{color:#ff6b6b;margin-top:8px}
</style>
</head>
<body>
<h1>HTN Stratum Bridge</h1>
<h2>Miner Earnings &amp; Stats</h2>
<p>Enter your wallet address to view historical block rewards.</p>
<form action="/stats" method="GET">
  <input type="text" name="address" placeholder="hoosat:qr…" required>
  <br>
  <button type="submit">View Stats</button>
</form>
</body>
</html>`))

// statsTmpl renders the per-wallet stats page.
var statsTmpl = template.Must(template.New("stats").Funcs(template.FuncMap{
	"fmtAtoms": func(atoms uint64) string {
		// Display as HTN with 8 decimal places (1 HTN = 1e8 atoms)
		htn := float64(atoms) / 1e8
		return fmt.Sprintf("%.8f HTN", htn)
	},
	"fmtTime": func(ms int64) string {
		if ms == 0 {
			return "–"
		}
		return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05 UTC")
	},
	"shortHash": func(h string) string {
		if len(h) > 16 {
			return h[:8] + "…" + h[len(h)-8:]
		}
		return h
	},
	// fmtHashrate formats a GH/s value using the same notation as the console stats.
	"fmtHashrate": func(ghs float64) string {
		return stringifyHashrate(ghs)
	},
	// isStale returns true when a block submitted more than 10 minutes ago still
	// has no confirmed reward, indicating it was likely orphaned.
	"isStale": func(timestampMs int64) bool {
		return time.Since(time.UnixMilli(timestampMs)) > 10*time.Minute
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>HTN Stratum Bridge – Stats for {{.Address}}</title>
<style>
  body{font-family:sans-serif;max-width:1100px;margin:40px auto;padding:0 16px;background:#1a1a2e;color:#eee}
  h1{color:#00d4ff}
  a{color:#00d4ff}
  .summary{display:flex;gap:24px;flex-wrap:wrap;margin:20px 0}
  .card{background:#16213e;border:1px solid #0f3460;border-radius:8px;padding:16px 24px;min-width:180px}
  .card .label{font-size:12px;color:#aaa}
  .card .value{font-size:22px;font-weight:bold;color:#00d4ff;margin-top:4px}
  table{width:100%;border-collapse:collapse;margin-top:24px}
  th{background:#0f3460;padding:10px 12px;text-align:left;font-size:13px}
  td{padding:8px 12px;border-bottom:1px solid #0f3460;font-size:13px}
  tr:hover td{background:#16213e}
  .badge-pending{color:#ffcc00}
  .badge-orphaned{color:#ff6b6b}
  .addr{word-break:break-all;font-size:13px;color:#aaa;margin-bottom:12px}
  .back{margin-bottom:16px}
  .copy-btn{background:none;border:none;color:#00d4ff;cursor:pointer;padding:0 4px;font-size:12px;vertical-align:middle;margin:0}
  .copy-btn:hover{color:#fff}
  h3{color:#00d4ff;margin-top:32px}
</style>
</head>
<body>
<h1>HTN Stratum Bridge</h1>
<div class="back"><a href="/">← Search another address</a></div>
<h2>Stats for</h2>
<div class="addr">{{.Address}}</div>

<div class="summary">
  <div class="card">
    <div class="label">Blocks Found</div>
    <div class="value">{{.TotalBlocks}}</div>
  </div>
  <div class="card">
    <div class="label">Total Earned</div>
    <div class="value">{{fmtAtoms .TotalAtoms}}</div>
  </div>
  <div class="card">
    <div class="label">Workers</div>
    <div class="value">{{.Workers}}</div>
  </div>
</div>

{{if .LiveWorkers}}
<h3>Current Workers</h3>
<table>
<thead>
<tr>
  <th>Worker</th>
  <th>Live Hashrate</th>
  <th>1h Hashrate</th>
  <th>24h Hashrate</th>
  <th>Blocks Found</th>
</tr>
</thead>
<tbody>
{{range .LiveWorkers}}
<tr>
  <td>{{.Name}}</td>
  <td>{{fmtHashrate .LiveGHs}}</td>
  <td>{{fmtHashrate .OneHrGHs}}</td>
  <td>{{fmtHashrate .TwentyFourHrGHs}}</td>
  <td>{{.BlocksFound}}</td>
</tr>
{{end}}
</tbody>
</table>
{{end}}

{{if .Blocks}}
<h3>Block History</h3>
<table>
<thead>
<tr>
  <th>#</th>
  <th>Time (UTC)</th>
  <th>Block Hash</th>
  <th>Worker</th>
  <th>Reward</th>
</tr>
</thead>
<tbody>
{{range $i, $b := .Blocks}}
<tr>
  <td>{{$b.ID}}</td>
  <td>{{fmtTime $b.Timestamp}}</td>
  <td title="{{$b.BlockHash}}">{{shortHash $b.BlockHash}}<button class="copy-btn" data-hash="{{$b.BlockHash}}" onclick="copyHash(this)" title="Copy full hash">⧉</button></td>
  <td>{{$b.WorkerName}}</td>
  <td>{{if eq $b.RewardAtoms 0}}{{if isStale $b.Timestamp}}<span class="badge-orphaned">orphaned</span>{{else}}<span class="badge-pending">pending</span>{{end}}{{else}}{{fmtAtoms $b.RewardAtoms}}{{end}}</td>
</tr>
{{end}}
</tbody>
</table>
{{else}}
<p>No blocks found for this address yet.</p>
{{end}}
<script>
function copyHash(btn) {
  const h = btn.getAttribute('data-hash');
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(h).then(function() {
      const orig = btn.textContent;
      btn.textContent = '\u2713';
      setTimeout(function() { btn.textContent = orig; }, 1500);
    }).catch(function() { fallbackCopy(btn, h); });
  } else {
    fallbackCopy(btn, h);
  }
}
function fallbackCopy(btn, h) {
  const t = document.createElement('textarea');
  t.value = h;
  t.style.position = 'fixed';
  t.style.opacity = '0';
  document.body.appendChild(t);
  t.select();
  document.execCommand('copy');
  document.body.removeChild(t);
  const orig = btn.textContent;
  btn.textContent = '\u2713';
  setTimeout(function() { btn.textContent = orig; }, 1500);
}
</script>
</body>
</html>`))

type statsPageData struct {
	Address     string
	Blocks      []BlockRecord
	TotalBlocks int
	TotalAtoms  uint64
	Workers     int
	LiveWorkers []WorkerLiveStat
}

// StartWebUI registers HTTP handlers and starts the web UI server on the given
// port (e.g. ":8080").  It is non-blocking: the listener runs in a goroutine.
// sh may be nil if no share handler has been created yet (workers section will
// simply be omitted from the stats page).
func StartWebUI(db *MiningDB, port string, logger *zap.SugaredLogger, sh *shareHandler) {
	mux := http.NewServeMux()

	// GET / — wallet address input form
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := indexTmpl.Execute(w, nil); err != nil {
			logger.Warn("webui: index template error", zap.Error(err))
		}
	})

	// GET /stats?address=<addr>&limit=<n> — HTML stats page
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		addr := strings.TrimSpace(r.URL.Query().Get("address"))
		if addr == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		limit := 200
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
				limit = n
			}
		}

		blocks, err := db.GetBlocksByWallet(addr, limit)
		if err != nil {
			logger.Warn("webui: db query error", zap.Error(err))
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}

		var totalAtoms uint64
		workers := map[string]struct{}{}
		for _, b := range blocks {
			totalAtoms += b.RewardAtoms
			if b.WorkerName != "" {
				workers[b.WorkerName] = struct{}{}
			}
		}

		filteredWorkers := getLiveWorkerStats(sh, addr)
		data := statsPageData{
			Address:     addr,
			Blocks:      blocks,
			TotalBlocks: len(blocks),
			TotalAtoms:  totalAtoms,
			Workers:     len(filteredWorkers),
			LiveWorkers: getLiveWorkerStats(sh, addr),
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := statsTmpl.Execute(w, data); err != nil {
			logger.Warn("webui: stats template error", zap.Error(err))
		}
	})

	// GET /api/blocks?address=<addr>&limit=<n> — JSON API
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		addr := strings.TrimSpace(r.URL.Query().Get("address"))
		if addr == "" {
			http.Error(w, `{"error":"missing address parameter"}`, http.StatusBadRequest)
			return
		}
		limit := 200
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
				limit = n
			}
		}

		blocks, err := db.GetBlocksByWallet(addr, limit)
		if err != nil {
			logger.Warn("webui: api db query error", zap.Error(err))
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		if blocks == nil {
			blocks = []BlockRecord{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(blocks); err != nil {
			logger.Warn("webui: json encode error", zap.Error(err))
		}
	})

	logger.Info("starting web UI on " + port)
	go func() {
		if err := http.ListenAndServe(port, mux); err != nil {
			logger.Error("web UI server error", zap.Error(err))
		}
	}()
}
