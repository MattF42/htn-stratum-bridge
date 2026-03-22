package htnstratum

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// WorkerLiveStat holds a point-in-time snapshot of live stats for one worker.
type WorkerLiveStat struct {
	Name            string  `json:"name"`
	LiveGHs         float64 `json:"live_ghs"`
	OneHrGHs        float64 `json:"one_hr_ghs"`
	TwentyFourHrGHs float64 `json:"twenty_four_hr_ghs"`
	BlocksFound     int64   `json:"blocks_found"`
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
  table{width:100%;border-collapse:collapse;margin-top:24px}
  table a{color:#00ff88;text-decoration:none} /* Bright cyan for unvisited */
  table a:visited{color:#ffaa00} /* Gold for visited */
  table a:hover{color:#ffffff} /* White on hover */
  th{background:#0f3460;padding:10px 12px;text-align:left;font-size:13px}
  td{padding:8px 12px;border-bottom:1px solid #0f3460;font-size:13px}
  tr:hover td{background:#16213e}
  #pools-table{width:90%;margin:0 auto}
  #pools-table a{color:#00ff88;text-decoration:none} /* Bright cyan for unvisited */
  #pools-table a:visited{color:#ffaa00} /* Gold for visited */
  #pools-table a:hover{color:#ffffff} /* White on hover */
  #ping-results{margin-top:16px;text-align:center;color:#00d4ff}
  #ping-results a{color:#00ff88;text-decoration:underline} /* Match table links */
  #ping-results a:visited{color:#ffaa00}
</style>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'%3E%3Ctext y='0.9em' font-size='90'%3E⛏️%3C/text%3E%3C/svg%3E">
</head>
<body>
<h1>HTN Solo Mining Pool</h1>
<h5>By Foztor {{printf "%.1f" .FeePercent}}% Pool Fee<BR>
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

<h3>Other HTN Pools like this</h3>
<table id="pools-table">
<thead>
<tr>
  <th>Pool URL</th>
  <th>Location</th>
  <th>Operator</th>
  <th>Ping (ms)</th>
</tr>
</thead>
<tbody id="pools-tbody">
<tr><td colspan="4">Loading pools...</td></tr>
</tbody>
</table>

<div id="ping-results"></div>

<script>
var currentStratum = "{{.StratumAddr}}";
var poolsData = []; // Store pool data

function pingFQDN(url) {
  return new Promise((resolve) => {
    const start = Date.now();
    const img = new Image();

    // We don't care if it succeeds or fails, just that the server responded
    img.onload = () => resolve(Date.now() - start);
    img.onerror = () => resolve(Date.now() - start);

    // Append a random string to prevent browser caching
    img.src = url + "/ping_test_" + Math.random();

    // Set a timeout so it doesn't hang forever
    setTimeout(() => resolve(-1), 5000); 
  });
}


fetch('https://htn-dl.foztor.net/pools.php')
  .then(response => {
    if (!response.ok) {
      throw new Error('Network response was not ok');
    }
    return response.json();
  })
  .then(data => {
    poolsData = data;
    let html = '';
    data.forEach(pool => {
      html += '<tr>' +
        '<td><a href="' + pool.pool_url + '">' + pool.pool_url + '</a></td>' +
        '<td>' + pool.location + '</td>' +
        '<td>' + pool.operator + '</td>' +
        '<td id="ping-' + pool.pool_url.replace(/[^a-zA-Z0-9]/g, '') + '">Pinging...</td>' +
        '</tr>';
    });
    document.getElementById('pools-tbody').innerHTML = html;

    // Ping each pool
    const pingPromises = data.map(pool => {
    // .origin includes protocol and port (e.g., http://some.domain.com:1234)
    const targetOrigin = new URL(pool.pool_url).origin;
  
    return pingFQDN(targetOrigin).then(latency => ({
    url: pool.pool_url,
    latency: latency
  }));
});

    Promise.all(pingPromises).then(results => {
      let minLatency = Infinity;
      let bestPool = null;
      const currentFQDN = window.location.hostname;  

      results.forEach(result => {
        const id = 'ping-' + result.url.replace(/[^a-zA-Z0-9]/g, '');
        const el = document.getElementById(id);
        if (el) {
          el.textContent = result.latency >= 0 ? result.latency + ' ms' : 'Error';
        }
        if (result.latency >= 0 && result.latency < minLatency) {
          minLatency = result.latency;
          bestPool = result.url;
        }
      });

      // Advice
      let advice = '';
      if (currentFQDN && bestPool) {
        const bestFQDN = new URL(bestPool).hostname;
        if (currentFQDN === bestFQDN) {
          advice = 'You are connected to the lowest latency pool!';
        } else {
          advice = 'Consider switching to <a href="' + bestPool + '">' + bestPool + '</a> for lower latency (' + minLatency + ' ms).';
        }
      } else if (bestPool) {
        advice = 'Lowest latency pool: <a href="' + bestPool + '">' + bestPool + '</a> (' + minLatency + ' ms).';
      }
      document.getElementById('ping-results').innerHTML = advice;
    });
  })
  .catch(error => {
    console.error('Error fetching pools:', error);
    document.getElementById('pools-tbody').innerHTML = '<tr><td colspan="4">Error loading pools.</td></tr>';
  });
</script>
<h3>Miner Downloads</h3>
<table id="miners-table">
<thead>
<tr>
  <th>Miner Name</th>
  <th>Supports</th>
  <th>Download URL</th>
</tr>
</thead>
<tbody>
<tr>
  <td>hoo_cpu</td>
  <td>AVX2 X64 CPU</td>
  <td><a href="https://htn.foztor.net/">https://htn.foztor.net/</a></td>
</tr>
<tr>
  <td>hoo_gpu</td>
  <td>NVIDIA CUDA GPUS Maxwell and above</td>
  <td><a href="https://htn.foztor.net/">https://htn.foztor.net/</a></td>
</tr>
<tr>
  <td>hoo_gpu_amd</td>
  <td>AMD ROCM GPUS - supports Vega  and above</td>
  <td><a href="https://htn.foztor.net/">https://htn.foztor.net/</a></td>
</tr>
<tr>
  <td>hoo_cpu_arm</td>
  <td>ARM64 Linux</td>
  <td><a href="https://htn.foztor.net/">https://htn.foztor.net/</a></td>
</tr>
<tr>
  <td>hoodroid</td>
  <td>Android 7+ 64 bit</td>
  <td><a href="https://hoodroid.htn.foztor.net/">https://hoodroid.htn.foztor.net/</a></td>
</tr>
<tr>
  <td>Hoominer</td>
  <td>Everything :) </td>
  <td><a href="https://github.com/HoosatNetwork/hoominer/releases">https://github.com/HoosatNetwork/hoominer/releases</a></td>
</tr>
</tbody>
</table>

<BR>

NB Only Hoominer has a native Windows binary, but all the others run fine under WSL<BR>
Hoominer is the reference miner.  The other miners implement a DevFee, and higher hashrate

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
  #refresh-overlay{display:none;position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.45);z-index:9999;align-items:center;justify-content:center}
  #refresh-overlay .refresh-box{background:#16213e;border:1px solid #0f3460;border-radius:8px;padding:20px 40px;color:#00d4ff;font-size:16px}
  .wbs-modal-overlay{display:none;position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.7);z-index:10000;align-items:center;justify-content:center}
  .wbs-modal-overlay.open{display:flex}
  .wbs-modal-box{background:#16213e;border:1px solid #0f3460;border-radius:8px;padding:24px;max-width:700px;width:95%;max-height:80vh;overflow-y:auto}
  .wbs-modal-box h3{color:#00d4ff;margin-top:0}
  .wbs-modal-close{float:right;background:none;border:none;color:#aaa;font-size:20px;cursor:pointer;line-height:1}
  .wbs-modal-close:hover{color:#fff}
  .wbs-modal-box table th.sortable{cursor:pointer;user-select:none;white-space:nowrap}
  .wbs-modal-box table th.sortable:hover{color:#00d4ff}
  .wbs-modal-box table th .sort-arrow{margin-left:4px;opacity:0.35;font-style:normal}
  .wbs-modal-box table th.sort-asc .sort-arrow,.wbs-modal-box table th.sort-desc .sort-arrow{opacity:1;color:#00d4ff}
  .wbs-link{color:#00d4ff;font-size:0.75em;font-weight:normal;text-decoration:underline;cursor:pointer}
</style>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'%3E%3Ctext y='0.9em' font-size='90'%3E⛏️%3C/text%3E%3C/svg%3E">
</head>
<body>
<div id="refresh-overlay"><div class="refresh-box">⟳ Refreshing…</div></div>
<h1>HTN Solo Mining Pool</h1>
<h5>By Foztor - {{printf "%.1f" .FeePercent}}% Pool Fee<BR><span id="reload-countdown">Data reload in 30 seconds</span></h5>
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
  <div class="value" id="lifetime-blocks-value">Blue: <span style="color: green;">{{.Blue}}</span> / Red: <span style="color: OrangeRed;">{{.Red}}</span> / Pending: <span style="color: orange;">{{.Pending}}</span> ({{printf "%.1f" .BluePercent}}%)</div>
</div>
  <div class="card">
    <div class="label">Lifetime Mined</div>
    <div class="value" id="lifetime-mined-value">{{fmtAtoms .TotalAtoms}}</div>
  </div>
  <div class="card">
    <div class="label">Current Workers</div>
    <div class="value" id="workers-count-value">{{.Workers}}</div>
  </div>
</div>
{{if .LiveWorkers}}
<div id="workers-section">
<h3>Live Workers (since restart)  → <a href="javascript:void(0)" onclick="openWorkerBlockModal()" class="wbs-link">Lifetime Per Worker Stats</a> </h3>
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
<tbody id="workers-tbody">
{{range .LiveWorkers}}
<tr>
  <td>{{.Name}}</td>
  <td>{{fmtHashrate .LiveGHs}}</td>
  <td>{{fmtHashrate .OneHrGHs}}</td>
  <td>{{fmtHashrate .TwentyFourHrGHs}}</td>
  <td>{{.BlocksFound}}</td>
</tr>
{{end}}
<tr id="workers-total-row" style="font-weight: bold; background: #0f3460;">
  <td>Total</td>
  <td>{{fmtHashrate .TotalLiveGHs}}</td>
  <td>{{fmtHashrate .TotalOneHrGHs}}</td>
  <td>{{fmtHashrate .TotalTwentyFourHrGHs}}</td>
  <td>{{.TotalBlocksFound}}</td>
</tr>
</tbody>
</table>
</div>
{{end}}

<h3>Block History</h3>
<div id="block-history">
<table>
<thead>
<tr>
  <th>#</th>
  <th>Time (UTC)</th>
  <th>Block Hash</th>
  <th>Worker</th>
  <th>Status</th>
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
 <!-- Put logic inside the TD but the title ON the TD -->
  {{if eq $b.Status "blue"}}
    <td title="Rewarded Block"><span style="color: DodgerBlue;">Blue</span></td>
  {{else if eq $b.Status "red"}}
    <td title="An Orphan in old money"><span style="color: IndianRed;">Red</span></td>
  {{else if eq $b.Status "merge_duplicate"}}
    <td title="Reward attributed to another block"><span style="color: cyan;">Merge Duplicate</span></td>
  {{else}}
    <td title="Waiting to assess DAG status"><span class="badge-pending">Pending</span></td>
  {{end}}
  <td>{{fmtAtoms $b.RewardAtoms}}</td>
</tr>
{{end}}
{{if not .Blocks}}
<tr><td colspan="6" style="text-align:center;color:#aaa">No blocks found for this address yet.</td></tr>
{{end}}
</tbody>
</table>
<div class="pagination" id="pagination-controls">
  <button id="btn-first" onclick="changePage('first')" disabled>First</button>
  <button id="btn-prev" onclick="changePage(-1)" disabled>← Previous</button>
  <span class="page-info" id="page-info">Page 1</span>
  <button id="btn-next" onclick="changePage(1)" disabled>Next →</button>
  <button id="btn-last" onclick="changePage('last')" disabled>Last</button>
</div>
</div>
<script>
// _addr is JS-escaped by html/template's contextual escaping (safe against XSS)
var _addr = "{{.Address}}";
var _total = {{.TotalBlocks}};
var _pageSize = 20;
var _offset = 0;

function changePage(dir) {
  var newOffset;
  if (dir === 'first') {
    newOffset = 0;
  } else if (dir === 'last') {
    var totalPages = Math.ceil(_total / _pageSize) || 1;
    newOffset = Math.max(0, (totalPages - 1) * _pageSize);
  } else {
    newOffset = _offset + dir * _pageSize;
  }

  if (newOffset < 0) newOffset = 0;
  if (dir > 0 && newOffset >= _total && dir !== 'last') return;  // Prevent invalid next

  if (newOffset === _offset) return;  // No change

  fetchPage(newOffset);
}

function fetchPage(offset) {
  var url = '/api/blocks?address=' + encodeURIComponent(_addr) +
            '&limit=' + _pageSize + '&offset=' + offset;
  fetch(url)
    .then(function(r) {
      var ct = r.headers.get('Content-Type') || '';
      if (!ct.includes('application/json')) {
        return r.text().then(function(text) {
          console.error('pagination fetch error: non-JSON response from', url,
                        'content-type=', ct,
                        'body prefix=', text.slice(0, 120));
          throw new Error('Non-JSON response');
        });
      }
      return r.json();
    })
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
    // Status cell
    var statusCell;
    if (b.Status === 'blue') {
      statusCell = '<span style="color: DodgerBlue;">Blue</span>';
    } else if (b.Status === 'red') {
      statusCell = '<span style="color: IndianRed;" title="An Orphan in old money">Red</span>';
    } else if (b.Status === 'merge_duplicate') {
      statusCell = '<span style="color: cyan;" title="Reward attributed to another block">Merge Duplicate</span>';
    } else {
      statusCell = '<span class="badge-pending" title="Waiting to assess DAG status">Pending</span>';
    }

    // Reward cell: just the atoms (0 for no reward)
    var rewardCell = fmtAtoms(b.RewardAtoms);

    // Show first 8 and last 8 chars for hashes longer than 16 chars (matches server-side shortHash)
    var shortH = b.BlockHash.length > 16 ? b.BlockHash.slice(0,8) + '\u2026' + b.BlockHash.slice(-8) : b.BlockHash;
    var tStr = b.Timestamp ? new Date(b.Timestamp).toISOString().replace('T',' ').replace(/\..+/,' UTC') : '\u2013';
      html += '<tr>' +
      '<td>' + b.ID + '</td>' +
      '<td>' + tStr + '</td>' +
      '<td title="' + escHtml(b.BlockHash) + '">' + escHtml(shortH) +
        '<button class="copy-btn" data-hash="' + escHtml(b.BlockHash) + '" onclick="copyHash(this)" title="Copy full hash">\u29C9</button></td>' +
      '<td>' + escHtml(b.WorkerName) + '</td>' +
      '<td>' + statusCell + '</td>' +  // New
      '<td>' + rewardCell + '</td>' +  // Simplified
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
  var paginationEl = document.getElementById('pagination-controls');
  if (!paginationEl) return;
  if (_total <= _pageSize) {
    paginationEl.style.display = 'none';
    return;
  } else {
    paginationEl.style.display = 'flex';
  }
  var pageInfoEl = document.getElementById('page-info');
  var btnFirst = document.getElementById('btn-first');
  var btnPrev = document.getElementById('btn-prev');
  var btnNext = document.getElementById('btn-next');
  var btnLast = document.getElementById('btn-last');
  
  var page = Math.floor(_offset / _pageSize) + 1;
  var totalPages = Math.ceil(_total / _pageSize) || 1;
  pageInfoEl.textContent = 'Page ' + page + ' of ' + totalPages;
  var isFirstPage = _offset <= 0;
  var isLastPage = _offset + _pageSize >= _total;
  btnFirst.disabled = isFirstPage;
  btnPrev.disabled = isFirstPage;
  btnNext.disabled = isLastPage;
  btnLast.disabled = isLastPage;
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

// ── Auto-refresh (every 30 s) ─────────────────────────────────────────────
function _showOverlay() {
  var el = document.getElementById('refresh-overlay');
  if (el) { el.style.display = 'flex'; }
}
function _hideOverlay() {
  var el = document.getElementById('refresh-overlay');
  if (el) { el.style.display = 'none'; }
}

// Mirror of Go stringifyHashrate: input is GH/s.
function _fmtHashrate(ghs) {
  var units = ['H/s','KH/s','MH/s','GH/s','TH/s','PH/s','EH/s','ZH/s','YH/s'];
  var hr, unit;
  if (ghs * 1000000 < 1) {
    hr = ghs * 1e9; unit = units[0];
  } else if (ghs * 1000 < 1) {
    hr = ghs * 1e6; unit = units[1];
  } else if (ghs < 1) {
    hr = ghs * 1000; unit = units[2];
  } else if (ghs < 1000) {
    hr = ghs; unit = units[3];
  } else {
    var divs = [1000, 1e6, 1e9, 1e12, 1e15];
    hr = ghs; unit = units[3];
    for (var i = 0; i < divs.length; i++) {
      var v = ghs / divs[i];
      if (v < 1000) { hr = v; unit = units[4 + i]; break; }
    }
  }
  return hr.toFixed(2).replace('.', ',') + unit;
}

function _applyStats(data) {
  // NetHash
  var nhEl = document.getElementById('nethash');
  if (nhEl) {
    var nh = data.net_hash || 0;
    nhEl.textContent = 'Net Hash: ' + (nh >= 1000 ? (nh / 1000).toFixed(2) + ' GH/s' : nh.toFixed(2) + ' MH/s');
  }
  // Lifetime Blocks
  var lbEl = document.getElementById('lifetime-blocks-value');
  if (lbEl) {
    lbEl.innerHTML = 'Blue: <span style="color:green;">' + (data.blue || 0) +
      '</span> / Red: <span style="color:red;">' + (data.red || 0) +
      '</span> / Pending: <span style="color:orange;">' + (data.pending || 0) +
      '</span> (' + ((data.blue_percent || 0).toFixed(1)) + '%)';
  }
  // Lifetime Mined
  var lmEl = document.getElementById('lifetime-mined-value');
  if (lmEl) {
    lmEl.textContent = ((data.total_atoms || 0) / 1e8).toFixed(8) + ' HTN';
  }
  // Workers count
  var wcEl = document.getElementById('workers-count-value');
  if (wcEl) {
    wcEl.textContent = data.workers || 0;
  }
  // Workers table body
  var wtbody = document.getElementById('workers-tbody');
  if (wtbody && data.live_workers && data.live_workers.length > 0) {
    var whtml = '';
    var totLive = 0, totOneHr = 0, tot24hr = 0, totBlocks = 0;
    for (var i = 0; i < data.live_workers.length; i++) {
      var w = data.live_workers[i];
      totLive += w.live_ghs || 0;
      totOneHr += w.one_hr_ghs || 0;
      tot24hr += w.twenty_four_hr_ghs || 0;
      totBlocks += w.blocks_found || 0;
      whtml += '<tr><td>' + escHtml(w.name) + '</td>' +
        '<td>' + _fmtHashrate(w.live_ghs || 0) + '</td>' +
        '<td>' + _fmtHashrate(w.one_hr_ghs || 0) + '</td>' +
        '<td>' + _fmtHashrate(w.twenty_four_hr_ghs || 0) + '</td>' +
        '<td>' + (w.blocks_found || 0) + '</td></tr>';
    }
    whtml += '<tr id="workers-total-row" style="font-weight:bold;background:#0f3460;">' +
      '<td>Total</td>' +
      '<td>' + _fmtHashrate(totLive) + '</td>' +
      '<td>' + _fmtHashrate(totOneHr) + '</td>' +
      '<td>' + _fmtHashrate(tot24hr) + '</td>' +
      '<td>' + totBlocks + '</td></tr>';
    wtbody.innerHTML = whtml;
  }
}

var _refreshIntervalMs = 30000;       // 30 seconds
var _countdownSeconds = _refreshIntervalMs / 1000;
var _countdownTimer = null;

function _updateCountdownLabel() {
  var el = document.getElementById('reload-countdown');
  if (!el) return;
  var s = _countdownSeconds;
  el.textContent = 'Data reload in ' + s + ' second' + (s === 1 ? '' : 's');
}

function _startCountdown() {
  // Reset to full interval
  _countdownSeconds = _refreshIntervalMs / 1000;
  _updateCountdownLabel();

  // Clear any previous timer
  if (_countdownTimer !== null) {
    clearInterval(_countdownTimer);
  }

  // Tick every second
  _countdownTimer = setInterval(function() {
    _countdownSeconds -= 1;
    if (_countdownSeconds <= 0) {
      _countdownSeconds = 0;
      _updateCountdownLabel();
      clearInterval(_countdownTimer);
      _countdownTimer = null;
      return;
    }
    _updateCountdownLabel();
  }, 1000);
}


function _refreshStats() {
  _showOverlay();
  var start = Date.now();
  var MIN_VISIBLE_MS = 1200;

  var p1 = fetch('/api/stats?address=' + encodeURIComponent(_addr)).then(r => r.json());
  var p2 = fetch('/api/blocks?address=' + encodeURIComponent(_addr) +
               '&limit=' + _pageSize + '&offset=' + _offset).then(r => r.json());

  Promise.all([p1, p2])
    .then(function(results) {
      _applyStats(results[0]);
      var blocksData = results[1];
      renderTable(blocksData.blocks);
      _total = blocksData.total;
      updatePagination();
      var elapsed = Date.now() - start;
      var remaining = MIN_VISIBLE_MS - elapsed;
      if (remaining > 0) {
        setTimeout(finish, remaining);
      } else {
        finish();
      }
    })
    .catch(function(err) {
      console.error('refresh error', err);
      var elapsed = Date.now() - start;
      var remaining = MIN_VISIBLE_MS - elapsed;
      if (remaining > 0) {
        setTimeout(finish, remaining);
      } else {
        finish();
      }
    });
}

function finish() {
  _hideOverlay();
  _startCountdown();
}


// Kick off first refresh shortly after page load
setTimeout(function() {
  _refreshStats();
}, 0);

// Also schedule automatic refreshes every 30s
setInterval(_refreshStats, _refreshIntervalMs);

// Start the initial countdown immediately on page load
_startCountdown();

// ── Worker Block Stats modal ───────────────────────────────────────────────
var _wbsData = [];
var _wbsSortCol = 'blue_percent'; // matches JSON field from /api/worker_counts; displayed as "% Blue"
var _wbsSortDir = 'desc';

var _wbsColMap = {
  'worker_name': 'wbs-th-name',
  'blue':        'wbs-th-blue',
  'red':         'wbs-th-red',
  'blue_percent':'wbs-th-pct'
};

function sortWbsTable(col) {
  if (_wbsSortCol === col) {
    _wbsSortDir = (_wbsSortDir === 'asc') ? 'desc' : 'asc';
  } else {
    _wbsSortCol = col;
    _wbsSortDir = (col === 'worker_name') ? 'asc' : 'desc';
  }
  renderWbsTable();
}

function renderWbsTable() {
  var tbody = document.getElementById('wbs-tbody');
  if (!tbody) return;

  // Update header sort indicators
  var cols = Object.keys(_wbsColMap);
  for (var c = 0; c < cols.length; c++) {
    var th = document.getElementById(_wbsColMap[cols[c]]);
    if (!th) continue;
    th.classList.remove('sort-asc', 'sort-desc');
    var arrow = th.querySelector('.sort-arrow');
    if (cols[c] === _wbsSortCol) {
      th.classList.add(_wbsSortDir === 'asc' ? 'sort-asc' : 'sort-desc');
      if (arrow) arrow.textContent = (_wbsSortDir === 'asc') ? '↑' : '↓';
    } else {
      if (arrow) arrow.textContent = '↕';
    }
  }

  if (!_wbsData || _wbsData.length === 0) {
    tbody.innerHTML = '<tr><td colspan="4" style="text-align:center;color:#aaa">No data found.</td></tr>';
    return;
  }

  var sorted = _wbsData.slice();
  var col = _wbsSortCol;
  var dir = _wbsSortDir;
  sorted.sort(function(a, b) {
    var av = a[col], bv = b[col];
    if (col === 'worker_name') {
      av = (av || '').toLowerCase();
      bv = (bv || '').toLowerCase();
      if (av < bv) return dir === 'asc' ? -1 : 1;
      if (av > bv) return dir === 'asc' ? 1 : -1;
      return 0;
    }
    av = av || 0;
    bv = bv || 0;
    return dir === 'asc' ? av - bv : bv - av;
  });

  var html = '';
  for (var i = 0; i < sorted.length; i++) {
    var wk = sorted[i];
    html += '<tr>' +
      '<td>' + escHtml(wk.worker_name) + '</td>' +
      '<td style="color:#4fc3f7">' + wk.blue + '</td>' +
      '<td style="color:#ef9a9a">' + wk.red + '</td>' +
      '<td>' + (wk.blue_percent || 0).toFixed(1) + '%</td>' +
      '</tr>';
  }
  tbody.innerHTML = html;
}

function openWorkerBlockModal() {
  document.getElementById('wbs-modal').classList.add('open');
  fetchWorkerBlockStats();
}

function closeWorkerBlockModal() {
  document.getElementById('wbs-modal').classList.remove('open');
}


function fetchWorkerBlockStats() {
  var tbody = document.getElementById('wbs-tbody');
  if (!tbody) return;
  tbody.innerHTML = '<tr><td colspan="4" style="text-align:center;color:#aaa">Loading…</td></tr>';
  fetch('/api/worker_counts?address=' + encodeURIComponent(_addr))
    .then(function(r) { return r.json(); })
    .then(function(data) {
      _wbsData = data || [];
      renderWbsTable();
    })
    .catch(function(err) {
      tbody.innerHTML = '<tr><td colspan="4" style="text-align:center;color:#ff6b6b">Error loading data.</td></tr>';
      console.error('worker block stats error', err);
    });
}

</script>

<div id="wbs-modal" class="wbs-modal-overlay">
  <div class="wbs-modal-box">
    <button class="wbs-modal-close" onclick="closeWorkerBlockModal()" title="Close">✕</button>
    <h3>Historical Per Worker Stats</h3>
    <table>
      <thead>
        <tr>
          <th class="sortable" id="wbs-th-name" onclick="sortWbsTable('worker_name')">Worker Name <span class="sort-arrow">↕</span></th>
          <th class="sortable" id="wbs-th-blue" onclick="sortWbsTable('blue')">Blue Blocks <span class="sort-arrow">↕</span></th>
          <th class="sortable" id="wbs-th-red" onclick="sortWbsTable('red')">Red Blocks <span class="sort-arrow">↕</span></th>
          <th class="sortable" id="wbs-th-pct" onclick="sortWbsTable('blue_percent')">% Blue <span class="sort-arrow">↕</span></th>
        </tr>
      </thead>
      <tbody id="wbs-tbody">
        <tr><td colspan="4" style="text-align:center;color:#aaa">Loading…</td></tr>
      </tbody>
    </table>
  </div>
</div>
<script>
document.getElementById('wbs-modal').addEventListener('click', function(e) {
  if (e.target === this) { closeWorkerBlockModal(); }
});
</script>
</body>
</html>`))

// WorkerBlockCount holds per-worker blue/red block counts used by the
// /api/worker_counts endpoint and populated by GetWorkerBlockCountsByWallet.
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
	FeePercent float64
}

// StartWebUI registers HTTP handlers and starts the web UI server on the given
// port (e.g. ":8080").  It is non-blocking: the listener runs in a goroutine.
// sh may be nil if no share handler has been created yet (workers section will
// simply be omitted from the stats page).
//
// When enableTLS is true and the certificate/key files load successfully, an
// HTTPS listener is started on httpsPort (defaulting to ":443") and the HTTP
// listener on port is repurposed to redirect all requests to HTTPS.  If TLS
// initialisation fails the function logs the error and falls back to plain
// HTTP on port.
func StartWebUI(db *MiningDB, port string, logger *zap.SugaredLogger, sh *shareHandler, stratumAddr string, feePPM int,
	enableTLS bool, tlsCertFile, tlsKeyFile, httpsPort string) {
        feePercent := float64(feePPM) / 100.0  // Convert PPM to percentage (e.g., 50 -> 0.5)

	mux := http.NewServeMux()

        // GET / — wallet address input form
    	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        	if r.URL.Path != "/" {
            	http.NotFound(w, r)
            	return
        	}
        	data := struct {
            	StratumAddr string
            	FeePercent  float64
        	}{StratumAddr: stratumAddr, FeePercent: feePercent}
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
                        FeePercent:           feePercent, 
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

	// GET /api/worker_counts?address=<addr> — per-worker blue/red block counts
	mux.HandleFunc("/api/worker_counts", func(w http.ResponseWriter, r *http.Request) {
		addr := strings.TrimSpace(r.URL.Query().Get("address"))
		if addr == "" {
			http.Error(w, `{"error":"missing address parameter"}`, http.StatusBadRequest)
			return
		}
		counts, err := db.GetWorkerBlockCountsByWallet(addr)
		if err != nil {
			logger.Warn("webui: api worker_counts db error", zap.Error(err))
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		if counts == nil {
			counts = []WorkerBlockCount{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(counts); err != nil {
			logger.Warn("webui: api worker_counts encode error", zap.Error(err))
		}
	})

	// GET /api/stats?address=<addr> — JSON API for the auto-refresh data
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		addr := strings.TrimSpace(r.URL.Query().Get("address"))
		if addr == "" {
			http.Error(w, `{"error":"missing address parameter"}`, http.StatusBadRequest)
			return
		}
		totalAtoms, err := db.GetTotalAtomsByWallet(addr)
		if err != nil {
			logger.Warn("webui: api stats atoms error", zap.Error(err))
			totalAtoms = 0
		}
		blue, red, pending := db.GetBlockCountsByWallet(addr)
		totalConfirmed := blue + red
		bluePercent := 0.0
		if totalConfirmed > 0 {
			bluePercent = float64(blue) / float64(totalConfirmed) * 100
		}
		workers := getLiveWorkerStats(sh, addr)
		if workers == nil {
			workers = []WorkerLiveStat{}
		}
		var totalLive, totalOneHr, total24Hr float64
		var totalBlocksFound int64
		for _, wk := range workers {
			totalLive += wk.LiveGHs
			totalOneHr += wk.OneHrGHs
			total24Hr += wk.TwentyFourHrGHs
			totalBlocksFound += wk.BlocksFound
		}
		netHash := 0.0
		if sh != nil {
			netHash = DiffToHash(sh.soloDiff) * 5 * 1000 // GH/s × 5 bps block rate × 1000 → MH/s network estimate
		}
		w.Header().Set("Content-Type", "application/json")
		resp := struct {
			NetHash             float64          `json:"net_hash"`
			TotalAtoms          uint64           `json:"total_atoms"`
			Workers             int              `json:"workers"`
			LiveWorkers         []WorkerLiveStat `json:"live_workers"`
			Blue                int              `json:"blue"`
			Red                 int              `json:"red"`
			Pending             int              `json:"pending"`
			BluePercent         float64          `json:"blue_percent"`
			TotalLiveGHs        float64          `json:"total_live_ghs"`
			TotalOneHrGHs       float64          `json:"total_one_hr_ghs"`
			TotalTwentyFourHrGHs float64         `json:"total_twenty_four_hr_ghs"`
			TotalBlocksFound    int64            `json:"total_blocks_found"`
		}{
			NetHash:              netHash,
			TotalAtoms:           totalAtoms,
			Workers:              len(workers),
			LiveWorkers:          workers,
			Blue:                 blue,
			Red:                  red,
			Pending:              pending,
			BluePercent:          bluePercent,
			TotalLiveGHs:         totalLive,
			TotalOneHrGHs:        totalOneHr,
			TotalTwentyFourHrGHs: total24Hr,
			TotalBlocksFound:     totalBlocksFound,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			logger.Warn("webui: api stats encode error", zap.Error(err))
		}
	})


	logger.Info("starting web UI on " + port)
	if enableTLS {
		if httpsPort == "" {
			httpsPort = ":443"
		}
		// Verify that the cert and key files are readable before committing to
		// TLS so we can fall back gracefully rather than crashing.
		certFile, certErr := os.Open(tlsCertFile)
		if certErr == nil {
			certFile.Close()
		}
		keyFile, keyErr := os.Open(tlsKeyFile)
		if keyErr == nil {
			keyFile.Close()
		}
		if certErr != nil || keyErr != nil {
			if certErr != nil {
				logger.Errorw("webui: TLS cert file not readable, falling back to HTTP", "file", tlsCertFile, zap.Error(certErr))
			}
			if keyErr != nil {
				logger.Errorw("webui: TLS key file not readable, falling back to HTTP", "file", tlsKeyFile, zap.Error(keyErr))
			}
			// Fall through to plain HTTP below.
			enableTLS = false
		}
	}

	if enableTLS {
		// Start the HTTPS listener.
		logger.Info("starting HTTPS web UI on " + httpsPort)
		go func() {
			if err := http.ListenAndServeTLS(httpsPort, tlsCertFile, tlsKeyFile, mux); err != nil {
				logger.Error("webui: HTTPS server error", zap.Error(err))
			}
		}()

		// Repurpose the HTTP port to redirect all traffic to HTTPS.
		httpsPortForRedirect := httpsPort
		redirectMux := http.NewServeMux()
		redirectMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Build the HTTPS target URL.  Strip the leading ":" from the port
			// when the port is the default 443 so we produce clean URLs.
			host := r.Host
			// If the host already contains a port, strip it.
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			target := "https://" + host
			if httpsPortForRedirect != ":443" && httpsPortForRedirect != "443" {
				target += httpsPortForRedirect
			}
			target += r.RequestURI
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		})
		go func() {
			if err := http.ListenAndServe(port, redirectMux); err != nil {
				logger.Error("webui: HTTP redirect server error", zap.Error(err))
			}
		}()
	} else {
		go func() {
			if err := http.ListenAndServe(port, mux); err != nil {
				logger.Error("web UI server error", zap.Error(err))
			}
		}()
	}
}
