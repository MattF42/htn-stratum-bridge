package pepepow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// RPCClient is a Bitcoin-compatible JSON-RPC client for PePe-core nodes.
type RPCClient struct {
	url      string
	user     string
	pass     string
	client   *http.Client
	reqID    uint64
}

// NewRPCClient creates a new Bitcoin JSON-RPC client.
func NewRPCClient(url, user, pass string) *RPCClient {
	return &RPCClient{
		url:  url,
		user: user,
		pass: pass,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

func (c *RPCClient) call(method string, params []any) (json.RawMessage, error) {
	id := atomic.AddUint64(&c.reqID, 1)
	req := rpcRequest{
		JSONRPC: "1.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RPC request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.user != "" || c.pass != "" {
		httpReq.SetBasicAuth(c.user, c.pass)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read RPC response: %w", err)
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal RPC response: %w (body: %s)", err, string(respBody))
	}
	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}
	return rpcResp.Result, nil
}

// BlockTemplate is the response from getblocktemplate.
type BlockTemplate struct {
	Version              uint32              `json:"version"`
	PreviousBlockHash    string              `json:"previousblockhash"`
	Transactions         []TemplateTransaction `json:"transactions"`
	CoinbaseAux          map[string]string   `json:"coinbaseaux"`
	CoinbaseValue        int64               `json:"coinbasevalue"`
	Target               string              `json:"target"`
	MinTime              int64               `json:"mintime"`
	Mutable              []string            `json:"mutable"`
	NonceRange           string              `json:"noncerange"`
	SigOpLimit           int                 `json:"sigoplimit"`
	SizeLimit            int                 `json:"sizelimit"`
	CurTime              int64               `json:"curtime"`
	Bits                 string              `json:"bits"`
	Height               int64               `json:"height"`
	DefaultWitnessCommit string              `json:"default_witness_commitment"`
}

// TemplateTransaction is a transaction in the block template.
type TemplateTransaction struct {
	Data    string `json:"data"`
	TxID    string `json:"txid"`
	Hash    string `json:"hash"`
	Fee     int64  `json:"fee"`
	SigOps  int    `json:"sigops"`
	Weight  int    `json:"weight"`
}

// GetBlockTemplate fetches a block template from the PePe-core node.
func (c *RPCClient) GetBlockTemplate() (*BlockTemplate, error) {
	// Request with standard capabilities
	params := []any{
		map[string]any{
			"rules": []string{"segwit"},
		},
	}
	result, err := c.call("getblocktemplate", params)
	if err != nil {
		return nil, err
	}
	var tmpl BlockTemplate
	if err := json.Unmarshal(result, &tmpl); err != nil {
		return nil, fmt.Errorf("failed to parse block template: %w", err)
	}
	return &tmpl, nil
}

// SubmitBlock submits a solved block (hex-encoded) to the node.
func (c *RPCClient) SubmitBlock(blockHex string) error {
	result, err := c.call("submitblock", []any{blockHex})
	if err != nil {
		return err
	}
	// submitblock returns null on success, or an error string
	var resultStr *string
	if err := json.Unmarshal(result, &resultStr); err == nil && resultStr != nil && *resultStr != "" {
		return fmt.Errorf("block rejected: %s", *resultStr)
	}
	return nil
}

// BlockchainInfo is the response from getblockchaininfo.
type BlockchainInfo struct {
	Chain                string  `json:"chain"`
	Blocks               int64   `json:"blocks"`
	Headers              int64   `json:"headers"`
	BestBlockHash        string  `json:"bestblockhash"`
	Difficulty           float64 `json:"difficulty"`
	InitialBlockDownload bool    `json:"initialblockdownload"`
}

// GetBlockchainInfo returns blockchain info for sync checking.
func (c *RPCClient) GetBlockchainInfo() (*BlockchainInfo, error) {
	result, err := c.call("getblockchaininfo", nil)
	if err != nil {
		return nil, err
	}
	var info BlockchainInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("failed to parse blockchain info: %w", err)
	}
	return &info, nil
}

// MiningInfo is the response from getmininginfo.
type MiningInfo struct {
	Blocks             int64   `json:"blocks"`
	Difficulty         float64 `json:"difficulty"`
	NetworkHashPS      float64 `json:"networkhashps"`
	PooledTx           int     `json:"pooledtx"`
}

// GetMiningInfo returns mining info.
func (c *RPCClient) GetMiningInfo() (*MiningInfo, error) {
	result, err := c.call("getmininginfo", nil)
	if err != nil {
		return nil, err
	}
	var info MiningInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("failed to parse mining info: %w", err)
	}
	return &info, nil
}

// IsSynced returns true if the node is fully synced.
func (c *RPCClient) IsSynced() (bool, error) {
	info, err := c.GetBlockchainInfo()
	if err != nil {
		return false, err
	}
	return !info.InitialBlockDownload && info.Blocks >= info.Headers-1, nil
}
