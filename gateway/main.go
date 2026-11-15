// Gateway Service — Trabalho GB - Sistemas Distribuídos
//
// Serviço HTTP que roda no macOS e orquestra o benchmark entre os dois protocolos.
// Recebe requisições do cliente (curl/browser) em localhost:3000 e as repassa
// ao Carrinho Service rodando no Omarchy, medindo as métricas de cada chamada.
//
// Endpoints:
//
//	POST /benchmark/iniciar          — inicia um benchmark assíncrono
//	GET  /benchmark/status           — consulta o progresso do benchmark em curso
//	GET  /benchmark/resultados       — baixa o CSV com os resultados
//	GET  /health                     — health check
//
// Variáveis de ambiente:
//
//	CARRINHO_HOST   IP/hostname do Omarchy (padrão: localhost)
//	GATEWAY_PORT    Porta deste serviço    (padrão: 3000)
package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	"trabalho-gb/internal/factory"
	"trabalho-gb/internal/metrics"
	pb "trabalho-gb/proto/cartpb"
)

// tamanhosCarrinho define os tamanhos de carrinho testados no benchmark.
var tamanhosCarrinho = []int{1, 10, 100, 1000, 10000}

// ─── Estado global do benchmark ──────────────────────────────────────────────

// estadoBenchmark armazena o estado de uma execução de benchmark em curso.
type estadoBenchmark struct {
	mu         sync.RWMutex
	rodando    bool
	progresso  int
	total      int
	medicoes   []metrics.Medicao
	iniciadoEm time.Time
	erroFatal  string
}

var estado = &estadoBenchmark{}

// ─── Configuração ─────────────────────────────────────────────────────────────

type config struct {
	carrinhoHost string
	portaGRPC    string
	baseHTTP     string
	gatewayPort  string
}

func carregarConfig() config {
	host := os.Getenv("CARRINHO_HOST")
	if host == "" {
		host = "localhost"
	}
	porta := os.Getenv("GATEWAY_PORT")
	if porta == "" {
		porta = "3000"
	}
	return config{
		carrinhoHost: host,
		portaGRPC:    host + ":50051",
		baseHTTP:     "http://" + host + ":8080",
		gatewayPort:  porta,
	}
}

// ─── Handlers HTTP ────────────────────────────────────────────────────────────

// handlerIniciar recebe os parâmetros do benchmark e o dispara em background.
//
// Body JSON (todos opcionais):
//
//	{ "iteracoes": 100, "aquecimento": 10 }
func handlerIniciar(cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "método não permitido", http.StatusMethodNotAllowed)
			return
		}

		estado.mu.Lock()
		if estado.rodando {
			estado.mu.Unlock()
			responderJSON(w, http.StatusConflict, map[string]string{
				"erro": "benchmark já em execução",
			})
			return
		}
		estado.mu.Unlock()

		// Parâmetros com valores padrão
		iteracoes := 50
		aquecimento := 5

		var params struct {
			Iteracoes   int `json:"iteracoes"`
			Aquecimento int `json:"aquecimento"`
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			if err := json.Unmarshal(body, &params); err == nil {
				if params.Iteracoes > 0 {
					iteracoes = params.Iteracoes
				}
				if params.Aquecimento >= 0 {
					aquecimento = params.Aquecimento
				}
			}
		}

		// Conexão gRPC (criada por benchmark, fechada ao fim)
		conn, err := grpc.NewClient(cfg.portaGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			responderJSON(w, http.StatusServiceUnavailable, map[string]string{
				"erro": fmt.Sprintf("falha ao conectar ao Carrinho Service: %v", err),
			})
			return
		}

		total := len(tamanhosCarrinho) * iteracoes * 2 * 2 // tamanhos × iterações × protocolos × operações

		estado.mu.Lock()
		estado.rodando = true
		estado.progresso = 0
		estado.total = total
		estado.medicoes = nil
		estado.iniciadoEm = time.Now()
		estado.erroFatal = ""
		estado.mu.Unlock()

		// Executa em background para não bloquear a resposta HTTP
		go func() {
			defer conn.Close()
			executarBenchmark(conn, cfg.baseHTTP, iteracoes, aquecimento)
		}()

		responderJSON(w, http.StatusAccepted, map[string]interface{}{
			"mensagem":    "benchmark iniciado",
			"iteracoes":   iteracoes,
			"aquecimento": aquecimento,
			"total":       total,
			"status_url":  "/benchmark/status",
			"resultado_url": "/benchmark/resultados",
		})
	}
}

// handlerStatus retorna o progresso atual do benchmark.
func handlerStatus(w http.ResponseWriter, r *http.Request) {
	estado.mu.RLock()
	defer estado.mu.RUnlock()

	pct := 0.0
	if estado.total > 0 {
		pct = float64(estado.progresso) / float64(estado.total) * 100
	}

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"rodando":      estado.rodando,
		"progresso":    estado.progresso,
		"total":        estado.total,
		"percentual":   fmt.Sprintf("%.1f%%", pct),
		"medicoes":     len(estado.medicoes),
		"iniciado_em":  estado.iniciadoEm.Format(time.RFC3339),
		"erro":         estado.erroFatal,
	})
}

// handlerResultados devolve o CSV com todas as medições coletadas.
// Retorna 204 se o benchmark ainda estiver em curso ou não tiver sido iniciado.
func handlerResultados(w http.ResponseWriter, r *http.Request) {
	estado.mu.RLock()
	rodando := estado.rodando
	medicoes := estado.medicoes
	estado.mu.RUnlock()

	if rodando {
		responderJSON(w, http.StatusPartialContent, map[string]string{
			"erro": "benchmark ainda em execução, aguarde a conclusão",
		})
		return
	}
	if len(medicoes) == 0 {
		responderJSON(w, http.StatusNoContent, map[string]string{
			"erro": "nenhum resultado disponível — inicie um benchmark primeiro",
		})
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=\"resultados.csv\"")
	escreverCSV(w, medicoes)
}

// ─── Execução do benchmark ────────────────────────────────────────────────────

// executarBenchmark roda o ciclo completo de medições e atualiza o estado global.
func executarBenchmark(conn *grpc.ClientConn, baseHTTP string, iteracoes, aquecimento int) {
	clienteGRPC := pb.NewCarrinhoServiceClient(conn)
	clienteHTTP := &http.Client{Timeout: 120 * time.Second}

	var todasMedicoes []metrics.Medicao

	for _, numItens := range tamanhosCarrinho {
		carrinhoID := fmt.Sprintf("cart-%d", numItens)
		log.Printf("📦 Testando carrinho com %d itens...", numItens)

		// Aquecimento — descartado
		for i := 0; i < aquecimento; i++ {
			_ = benchGRPCObterCarrinho(clienteGRPC, carrinhoID, numItens, -1)
			_ = benchHTTPObterCarrinho(clienteHTTP, baseHTTP, carrinhoID, numItens, -1)
		}

		// Iterações registradas
		for i := 0; i < iteracoes; i++ {
			medicoes := []metrics.Medicao{
				benchGRPCObterCarrinho(clienteGRPC, carrinhoID, numItens, i),
				benchGRPCCheckout(clienteGRPC, carrinhoID, numItens, i),
				benchHTTPObterCarrinho(clienteHTTP, baseHTTP, carrinhoID, numItens, i),
				benchHTTPCheckout(clienteHTTP, baseHTTP, carrinhoID, numItens, i),
			}
			todasMedicoes = append(todasMedicoes, medicoes...)

			estado.mu.Lock()
			estado.progresso += len(medicoes)
			estado.mu.Unlock()
		}
	}

	estado.mu.Lock()
	estado.medicoes = todasMedicoes
	estado.rodando = false
	estado.mu.Unlock()

	log.Printf("✅ Benchmark concluído. Total de medições: %d", len(todasMedicoes))
}

// ─── Benchmarks gRPC ──────────────────────────────────────────────────────────

func benchGRPCObterCarrinho(c pb.CarrinhoServiceClient, carrinhoID string, numItens, iter int) metrics.Medicao {
	req := &pb.ObterCarrinhoRequest{CarrinhoId: carrinhoID}

	tSerInicio := time.Now()
	reqBytes, _ := proto.Marshal(req)
	tSerReq := time.Since(tSerInicio)

	tInicio := time.Now()
	resp, err := c.ObterCarrinho(context.Background(), req)
	tTotal := time.Since(tInicio)

	tDeserInicio := time.Now()
	respBytes, _ := proto.Marshal(resp)
	tDeser := time.Since(tDeserInicio)

	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	return metrics.Medicao{
		Protocolo:         metrics.GRPC,
		Operacao:          metrics.OpObterCarrinho,
		NumItens:          numItens,
		Iteracao:          iter,
		TempoTotal:        tTotal,
		TempoSerializacao: tSerReq + tDeser,
		TamanhoPayload:    len(reqBytes) + len(respBytes),
		TamanhoRequisicao: len(reqBytes),
		TamanhoResposta:   len(respBytes),
		Erro:              errStr,
		Timestamp:         time.Now(),
	}
}

func benchGRPCCheckout(c pb.CarrinhoServiceClient, carrinhoID string, numItens, iter int) metrics.Medicao {
	req := &pb.CheckoutRequest{
		CarrinhoId:      carrinhoID,
		EnderecoEntrega: factory.NovoEndereco(iter),
		MetodoPagamento: "cartao_credito",
	}

	tSerInicio := time.Now()
	reqBytes, _ := proto.Marshal(req)
	tSerReq := time.Since(tSerInicio)

	tInicio := time.Now()
	resp, err := c.Checkout(context.Background(), req)
	tTotal := time.Since(tInicio)

	tDeserInicio := time.Now()
	respBytes, _ := proto.Marshal(resp)
	tDeser := time.Since(tDeserInicio)

	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	return metrics.Medicao{
		Protocolo:         metrics.GRPC,
		Operacao:          metrics.OpCheckout,
		NumItens:          numItens,
		Iteracao:          iter,
		TempoTotal:        tTotal,
		TempoSerializacao: tSerReq + tDeser,
		TamanhoPayload:    len(reqBytes) + len(respBytes),
		TamanhoRequisicao: len(reqBytes),
		TamanhoResposta:   len(respBytes),
		Erro:              errStr,
		Timestamp:         time.Now(),
	}
}

// ─── Benchmarks HTTP/JSON ─────────────────────────────────────────────────────

func benchHTTPObterCarrinho(c *http.Client, base, carrinhoID string, numItens, iter int) metrics.Medicao {
	url := fmt.Sprintf("%s/carrinho/obter?carrinho_id=%s", base, carrinhoID)
	reqBytes := []byte(url)

	tInicio := time.Now()
	resp, err := c.Get(url)
	if err != nil {
		return medicaoErro(metrics.JSON, metrics.OpObterCarrinho, numItens, iter, err.Error())
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	tTotal := time.Since(tInicio)

	tDeserInicio := time.Now()
	var resultado map[string]interface{}
	_ = json.Unmarshal(respBody, &resultado)
	tDeser := time.Since(tDeserInicio)

	return metrics.Medicao{
		Protocolo:         metrics.JSON,
		Operacao:          metrics.OpObterCarrinho,
		NumItens:          numItens,
		Iteracao:          iter,
		TempoTotal:        tTotal,
		TempoSerializacao: tDeser,
		TamanhoPayload:    len(reqBytes) + len(respBody),
		TamanhoRequisicao: len(reqBytes),
		TamanhoResposta:   len(respBody),
		Erro:              "",
		Timestamp:         time.Now(),
	}
}

func benchHTTPCheckout(c *http.Client, base, carrinhoID string, numItens, iter int) metrics.Medicao {
	end := factory.NovoEndereco(iter)
	payload := map[string]interface{}{
		"carrinho_id":      carrinhoID,
		"endereco_entrega": end,
		"metodo_pagamento": "cartao_credito",
	}

	tSerInicio := time.Now()
	reqBody, _ := json.Marshal(payload)
	tSerReq := time.Since(tSerInicio)

	tInicio := time.Now()
	resp, err := c.Post(base+"/carrinho/checkout", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return medicaoErro(metrics.JSON, metrics.OpCheckout, numItens, iter, err.Error())
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	tTotal := time.Since(tInicio)

	tDeserInicio := time.Now()
	var resultado map[string]interface{}
	_ = json.Unmarshal(respBody, &resultado)
	tDeser := time.Since(tDeserInicio)

	return metrics.Medicao{
		Protocolo:         metrics.JSON,
		Operacao:          metrics.OpCheckout,
		NumItens:          numItens,
		Iteracao:          iter,
		TempoTotal:        tTotal,
		TempoSerializacao: tSerReq + tDeser,
		TamanhoPayload:    len(reqBody) + len(respBody),
		TamanhoRequisicao: len(reqBody),
		TamanhoResposta:   len(respBody),
		Erro:              "",
		Timestamp:         time.Now(),
	}
}

// ─── Utilitários ──────────────────────────────────────────────────────────────

func medicaoErro(proto metrics.Protocolo, op metrics.Operacao, numItens, iter int, errStr string) metrics.Medicao {
	return metrics.Medicao{
		Protocolo: proto,
		Operacao:  op,
		NumItens:  numItens,
		Iteracao:  iter,
		Erro:      errStr,
		Timestamp: time.Now(),
	}
}

// escreverCSV serializa as medições diretamente no ResponseWriter.
func escreverCSV(w io.Writer, medicoes []metrics.Medicao) {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{
		"protocolo", "operacao", "num_itens", "iteracao",
		"tempo_total_us", "tempo_serializacao_us",
		"tamanho_payload_bytes", "tamanho_requisicao_bytes", "tamanho_resposta_bytes",
		"erro", "timestamp",
	})
	for _, m := range medicoes {
		_ = cw.Write([]string{
			string(m.Protocolo),
			string(m.Operacao),
			strconv.Itoa(m.NumItens),
			strconv.Itoa(m.Iteracao),
			strconv.FormatInt(m.TempoTotal.Microseconds(), 10),
			strconv.FormatInt(m.TempoSerializacao.Microseconds(), 10),
			strconv.Itoa(m.TamanhoPayload),
			strconv.Itoa(m.TamanhoRequisicao),
			strconv.Itoa(m.TamanhoResposta),
			m.Erro,
			m.Timestamp.Format(time.RFC3339Nano),
		})
	}
}

func responderJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	cfg := carregarConfig()

	log.Printf("🚀 Gateway Service iniciando na porta :%s", cfg.gatewayPort)
	log.Printf("   Carrinho Service: %s (gRPC) | %s (HTTP)", cfg.portaGRPC, cfg.baseHTTP)

	mux := http.NewServeMux()
	mux.HandleFunc("/benchmark/iniciar",    handlerIniciar(cfg))
	mux.HandleFunc("/benchmark/status",     handlerStatus)
	mux.HandleFunc("/benchmark/resultados", handlerResultados)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		responderJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	log.Printf("   Endpoints disponíveis:")
	log.Printf("     POST http://localhost:%s/benchmark/iniciar", cfg.gatewayPort)
	log.Printf("     GET  http://localhost:%s/benchmark/status", cfg.gatewayPort)
	log.Printf("     GET  http://localhost:%s/benchmark/resultados", cfg.gatewayPort)

	if err := http.ListenAndServe(":"+cfg.gatewayPort, mux); err != nil {
		log.Fatalf("falha ao iniciar gateway: %v", err)
	}
}
