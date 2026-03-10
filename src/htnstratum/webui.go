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
	// "log"

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
		// log.Printf("DEBUG: Query addr = %q", addr)


	out := make([]WorkerLiveStat, 0, len(sh.stats))
	for _, v := range sh.stats {
				// log.Printf("DEBUG: Worker %q has WalletAddr %q", v.WorkerName, v.WalletAddr)
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
		// log.Printf("DEBUG: Found %d matching workers", len(out))

	return out
}

// indexTmpl is the wallet-address lookup form shown at GET /.
var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>HTN Solo Mining Pool Miner Stats</title>
<style>
  body{font-family:sans-serif;max-width:900px;margin:40px auto;padding:0 16px;background:#1a1a2e;color:#eee}
  h1{color:#00d4ff}
  input{padding:8px 12px;width:100%;box-sizing:border-box;background:#16213e;border:1px solid #0f3460;color:#eee;border-radius:4px;font-size:14px}
  button{margin-top:10px;padding:10px 24px;background:#0f3460;color:#eee;border:none;border-radius:4px;cursor:pointer;font-size:14px}
  button:hover{background:#00d4ff;color:#1a1a2e}
  .error{color:#ff6b6b;margin-top:8px}
</style>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'%3E%3Ctext y='0.9em' font-size='90'%3E⛏️%3C/text%3E%3C/svg%3E">
</head>
<body>
<h1>HTN Solo Mining Pool</h1>
<h5>By Foztor 0.5% Pool Fee<BR>
Pool fee is collected by randomly selecting jobs to mine to the pool wallet address<BR>
All blocks that turn blue are instantly available in your wallet<BR>
They are directly mined by you</h5>
<h2>Miner Earnings &amp; Stats</h2>
<p>Connect your miner to <code>{{.StratumAddr}}</code></p>
<p>Enter your wallet address to see your stats.</p>
<form action="/stats" method="GET">
  <input type="text" name="address" placeholder="hoosat:qr…" required>
  <br>
  <button type="submit">View Stats</button>
</form>
</body>
</html>`))

// statsTmpl renders the per-wallet stats page.
var statsTmpl = template.Must(template.New("stats").Funcs(template.FuncMap{
	"div": func(a, b float64) float64 { return a / b },
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
<title>HTN Solo Mining Pool – Stats for {{.Address}}</title>
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
  .addr{word-break:break-all;font-size:15px;color:#aaa;margin-bottom:12px}
  .back{margin-bottom:16px}
  .copy-btn{background:none;border:none;color:#00d4ff;cursor:pointer;padding:0 4px;font-size:12px;vertical-align:middle;margin:0}
  .copy-btn:hover{color:#fff}
  h3{color:#00d4ff;margin-top:32px}
  .pagination{display:flex;align-items:center;gap:12px;margin-top:16px}
  .pagination button{padding:8px 18px;background:#0f3460;color:#eee;border:none;border-radius:4px;cursor:pointer;font-size:13px}
  .pagination button:hover:not(:disabled){background:#00d4ff;color:#1a1a2e}
  .pagination button:disabled{opacity:0.4;cursor:default}
  .pagination .page-info{font-size:13px;color:#aaa}
</style>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'%3E%3Ctext y='0.9em' font-size='90'%3E⛏️%3C/text%3E%3C/svg%3E">
<meta http-equiv="refresh" content="60">
</head>
<body>
<h1>HTN Solo Mining Pool</h1>
<h5>By Foztor - 0.5% Pool Fee<BR>Page automatically reloads every 30 seconds</h5>
<div class="back"><a href="/">← Change Wallet</a></div>
<h2><span class="addr">Stats for: {{.Address}}</span> <span id="copyIcon" onclick="copyToClipboard('{{.Address}}')" style="cursor: pointer; margin-left: 5px;" title="Copy to clipboard">⧉</span></h2>
<div id="ping" style="position: fixed; top: 10px; right: 10px; background: #1a1a2e; color: #eee; padding: 5px; border-radius: 4px;">Ping: -- ms</div>
<div id="nethash" style="position: fixed; top: 40px; right: 10px; background: #1a1a2e; color: #eee; padding: 5px; border-radius: 4px; font-size: 16px;">
  Net Hash: {{if ge .NetHash 1000.0}}{{printf "%.2f GH/s" (div .NetHash 1000.0)}}{{else}}{{printf "%.2f MH/s" .NetHash}}{{end}}
</div>
<script>
function updatePing() {
    const start = Date.now();
    fetch(window.location.href, {method: 'HEAD'}).then(() => {
        const end = Date.now();
        document.getElementById('ping').textContent = 'Ping between browser and pool: ' + (end - start) + ' ms';
    }).catch(() => {
        document.getElementById('ping').textContent = 'Ping: error';
    });
}
setInterval(updatePing, 15000);
updatePing();

function copyToClipboard() {
    navigator.clipboard.writeText('{{.Address}}').then(() => {
        const icon = document.getElementById('copyIcon');
        icon.textContent = '✓';
        setTimeout(() => icon.textContent = '⧉', 2000);
    }).catch(err => {
        console.error('Failed to copy: ', err);
    });
}
</script>

<div class="summary">
<div class="card">
  <div class="label">Lifetime Blocks</div>
  <div class="value">Blue: <span style="color: green;">{{.Blue}}</span> / Red: <span style="color: red;">{{.Red}}</span> / Pending: <span style="color: orange;">{{.Pending}}</span> ({{printf "%.1f" .BluePercent}}%)</div>
</div>
  <div class="card">
    <div class="label">Lifetime Mined</div>
    <div class="value">{{fmtAtoms .TotalAtoms}}</div>
  </div>
  <div class="card">
    <div class="label">Current Workers</div>
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
<tr style="font-weight: bold; background: #0f3460;">
  <td>Total</td>
  <td>{{fmtHashrate .TotalLiveGHs}}</td>
  <td>{{fmtHashrate .TotalOneHrGHs}}</td>
  <td>{{fmtHashrate .TotalTwentyFourHrGHs}}</td>
  <td>{{.TotalBlocksFound}}</td>
</tr>
</tbody>
</table>
{{end}}

{{if .Blocks}}
<h3>Block History</h3>
<div id="block-history">
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
<tbody id="block-tbody">
{{range $i, $b := .Blocks}}
<tr>
  <td>{{$b.ID}}</td>
  <td>{{fmtTime $b.Timestamp}}</td>
  <td title="{{$b.BlockHash}}">{{shortHash $b.BlockHash}}<button class="copy-btn" data-hash="{{$b.BlockHash}}" onclick="copyHash(this)" title="Copy full hash">⧉</button></td>
  <td>{{$b.WorkerName}}</td>
  <td>{{if eq $b.RewardAtoms 0}}{{if isStale $b.Timestamp}}<span style="color:red;">RED BLOCK</span>{{else}}<span class="badge-pending">Pending</span>{{end}}{{else}}<span style="color: green;">{{fmtAtoms $b.RewardAtoms}}</span>{{end}}</td>
</tr>
{{end}}
</tbody>
</table>
<div class="pagination">
  <button id="btn-prev" onclick="changePage(-1)" disabled>← Previous</button>
  <span class="page-info" id="page-info">Page 1</span>
  <button id="btn-next" onclick="changePage(1)" {{if le .TotalBlocks 20}}disabled{{end}}>Next →</button>
</div>
</div>
{{else}}
<p>No blocks found for this address yet.</p>
{{end}}
<script>
// _addr is JS-escaped by html/template's contextual escaping (safe against XSS)
var _addr = "{{.Address}}";
var _total = {{.TotalBlocks}};
var _pageSize = 20;
var _offset = 0;

function changePage(dir) {
  var newOffset = _offset + dir * _pageSize;
  if (newOffset < 0) newOffset = 0;
  if (newOffset >= _total) return;
  fetchPage(newOffset);
}

function fetchPage(offset) {
  var url = '/api/blocks?address=' + encodeURIComponent(_addr) + '&limit=' + _pageSize + '&offset=' + offset;
  fetch(url)
    .then(function(r) { return r.json(); })
    .then(function(data) {
      _offset = offset;
      _total = data.total;
      renderTable(data.blocks);
      updatePagination();
    })
    .catch(function(err) { console.error('pagination fetch error', err); });
}

function renderTable(blocks) {
  var tbody = document.getElementById('block-tbody');
  if (!tbody) return;
  var html = '';
  for (var i = 0; i < blocks.length; i++) {
    var b = blocks[i];
    // console.log("status is " + b.status + " or " + b.Status);
    var rewardCell;
    var nowMs = Date.now();
    // Timestamp is Unix milliseconds; blocks older than 10 min with no reward are orphaned
    var ageMin = (nowMs - b.Timestamp) / 60000;
    if (b.Status === 'blue') {
    rewardCell = '<span style="color: green;">' + fmtAtoms(b.RewardAtoms) + '</span>';
  } else if (b.Status === 'red') {
    rewardCell = '<span style="color: red;">RED BLOCK</span>';
  } else {
    // For pending, check if stale (older than 10 min)
    var nowMs = Date.now();
    var ageMin = (nowMs - b.Timestamp) / 60000;
    if (ageMin > 10) {
	    rewardCell = '<span style="color: red;">RED BLOCK</span>';
    } else {
      rewardCell = '<span class="badge-pending">Pending</span>';
    }
  }
    // Show first 8 and last 8 chars for hashes longer than 16 chars (matches server-side shortHash)
    var shortH = b.BlockHash.length > 16 ? b.BlockHash.slice(0,8) + '\u2026' + b.BlockHash.slice(-8) : b.BlockHash;
    var tStr = b.Timestamp ? new Date(b.Timestamp).toISOString().replace('T',' ').replace(/\..+/,' UTC') : '\u2013';
    html += '<tr>' +
      '<td>' + b.ID + '</td>' +
      '<td>' + tStr + '</td>' +
      '<td title="' + escHtml(b.BlockHash) + '">' + escHtml(shortH) +
        '<button class="copy-btn" data-hash="' + escHtml(b.BlockHash) + '" onclick="copyHash(this)" title="Copy full hash">\u29C9</button></td>' +
      '<td>' + escHtml(b.WorkerName) + '</td>' +
      '<td>' + rewardCell + '</td>' +
      '</tr>';
  }
  tbody.innerHTML = html;
}

function fmtAtoms(atoms) {
  return (atoms / 1e8).toFixed(8) + ' HTN';
}

function escHtml(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

function updatePagination() {
  var page = Math.floor(_offset / _pageSize) + 1;
  var totalPages = Math.ceil(_total / _pageSize) || 1;
  document.getElementById('page-info').textContent = 'Page ' + page + ' of ' + totalPages;
  document.getElementById('btn-prev').disabled = _offset <= 0;
  document.getElementById('btn-next').disabled = _offset + _pageSize >= _total;
}

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

updatePagination();
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
        Blue        int     // New
        Red         int     // New
        Pending     int     // New
        BluePercent float64 // New
	TotalLiveGHs         float64
        TotalOneHrGHs        float64
        TotalTwentyFourHrGHs float64
        TotalBlocksFound     int64
	NetHash float64
}

// StartWebUI registers HTTP handlers and starts the web UI server on the given
// port (e.g. ":8080").  It is non-blocking: the listener runs in a goroutine.
// sh may be nil if no share handler has been created yet (workers section will
// simply be omitted from the stats page).
func StartWebUI(db *MiningDB, port string, logger *zap.SugaredLogger, sh *shareHandler, stratumAddr string) {
	mux := http.NewServeMux()

	// GET / — wallet address input form
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data := struct{ StratumAddr string }{StratumAddr: stratumAddr}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := indexTmpl.Execute(w, data); err != nil {
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
		const pageSize = 20

		blocks, err := db.GetBlocksByWalletPaged(addr, pageSize, 0)
		if err != nil {
			logger.Warn("webui: db query error", zap.Error(err))
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}

		totalBlocks, err := db.CountBlocksByWallet(addr)
		if err != nil {
			logger.Warn("webui: db count error", zap.Error(err))
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}

		var totalAtoms uint64
		totalAtoms, err = db.GetTotalAtomsByWallet(addr)
		if err != nil {
    		logger.Warn("webui: db total atoms error", zap.Error(err))
    		totalAtoms = 0  // Or handle error
		}

		filteredWorkers := getLiveWorkerStats(sh, addr)
                var totalLive, totalOneHr, total24Hr float64
		var alltotalBlocks int64
		for _, w := range filteredWorkers {
    		totalLive += w.LiveGHs
    		totalOneHr += w.OneHrGHs
    		total24Hr += w.TwentyFourHrGHs
    		alltotalBlocks += w.BlocksFound
		}
                blue, red, pending := db.GetBlockCountsByWallet(addr)  // New
                totalConfirmed := blue + red
                bluePercent := 0.0
                if totalConfirmed > 0 {
                    bluePercent = float64(blue) / float64(totalConfirmed) * 100
                }
                netHash := 0.0
                netHash = DiffToHash(sh.soloDiff) * 5 * 1000  // GH/s * bps to MH/s
		data := statsPageData{
			Address:     addr,
			Blocks:      blocks,
			TotalBlocks: totalBlocks,
			TotalAtoms:  totalAtoms,
			Workers:     len(filteredWorkers),
			LiveWorkers: getLiveWorkerStats(sh, addr),
                        Blue:        blue,
    			Red:         red, 
    			Pending:     pending,
    			BluePercent: bluePercent,
                        TotalLiveGHs:         totalLive,
    			TotalOneHrGHs:        totalOneHr,
                        TotalTwentyFourHrGHs: total24Hr,
                        TotalBlocksFound:     alltotalBlocks,
			NetHash: netHash,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := statsTmpl.Execute(w, data); err != nil {
			logger.Warn("webui: stats template error", zap.Error(err))
		}
	})

	// GET /api/blocks?address=<addr>&limit=<n>&offset=<n> — JSON API
	mux.HandleFunc("/api/blocks", func(w http.ResponseWriter, r *http.Request) {
		addr := strings.TrimSpace(r.URL.Query().Get("address"))
		if addr == "" {
			http.Error(w, `{"error":"missing address parameter"}`, http.StatusBadRequest)
			return
		}
		limit := 20
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
				limit = n
			}
		}
		offset := 0
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		total, err := db.CountBlocksByWallet(addr)
		if err != nil {
			logger.Warn("webui: api db count error", zap.Error(err))
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}

		blocks, err := db.GetBlocksByWalletPaged(addr, limit, offset)
		if err != nil {
			logger.Warn("webui: api db query error", zap.Error(err))
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		if blocks == nil {
			blocks = []BlockRecord{}
		}
		w.Header().Set("Content-Type", "application/json")
		resp := struct {
			Total  int           `json:"total"`
			Blocks []BlockRecord `json:"blocks"`
		}{Total: total, Blocks: blocks}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			logger.Warn("webui: json encode error", zap.Error(err))
		}
	})
        // GET /api/block_counts?address=<addr> — JSON API for block status counts
	mux.HandleFunc("/api/block_counts", func(w http.ResponseWriter, r *http.Request) {
    	addr := strings.TrimSpace(r.URL.Query().Get("address"))
    	if addr == "" {
        	http.Error(w, `{"error":"missing address parameter"}`, http.StatusBadRequest)
        	return
    	}
    	blue, red, pending := db.GetBlockCountsByWallet(addr)  // Implement in mining_db.go
    	totalConfirmed := blue + red
    	bluePercent := 0.0
    	if totalConfirmed > 0 {
        	bluePercent = float64(blue) / float64(totalConfirmed) * 100
    	}
    	w.Header().Set("Content-Type", "application/json")
    	json.NewEncoder(w).Encode(map[string]interface{}{
        	"blue":         blue,
        	"red":          red,
        	"pending":      pending,
        	"blue_percent": bluePercent,
    	})
	})


	logger.Info("starting web UI on " + port)
	go func() {
		if err := http.ListenAndServe(port, mux); err != nil {
			logger.Error("web UI server error", zap.Error(err))
		}
	}()
}
