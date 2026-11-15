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

// tamanhosCarrinho define os tamanhos de carrinho testados no benchmark serial.
var tamanhosCarrinho = []int{1, 10, 100, 1000, 10000}

// niveisConcorrencia define o número de workers simultâneos no teste de multiplexing.
var niveisConcorrencia = []int{1, 5, 10, 25, 50, 100}

// numItensConcorrencia é o tamanho de carrinho fixo usado no teste de multiplexing.
// Escolhemos 100 itens — payload médio, representativo de uso real.
const numItensConcorrencia = 100

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

// estadoConcorrencia armazena o progresso do teste de multiplexing.
type estadoConcorrenciaBenchmark struct {
	mu         sync.RWMutex
	rodando    bool
	progresso  int
	total      int
	medicoes   []metrics.MedicaoConcorrencia
	iniciadoEm time.Time
	erroFatal  string
}

var estadoConc = &estadoConcorrenciaBenchmark{}

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

// pastaResultados é onde os CSVs são salvos automaticamente ao fim de cada benchmark.
const pastaResultados = "resultados"

// executarBenchmark roda o ciclo completo de medições e atualiza o estado global.
func executarBenchmark(conn *grpc.ClientConn, baseHTTP string, iteracoes, aquecimento int) {
	clienteGRPC := pb.NewCarrinhoServiceClient(conn)
	clienteHTTP := &http.Client{Timeout: 120 * time.Second}

	var todasMedicoes []metrics.Medicao

	for _, numItens := range tamanhosCarrinho {
		carrinhoID := fmt.Sprintf("cart-%d", numItens)
		log.Printf("Testando carrinho com %d itens...", numItens)

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

	// Salva CSV em disco automaticamente
	nomeArquivo := fmt.Sprintf("%s/serial_%s.csv", pastaResultados, time.Now().Format("20060102_150405"))
	if err := metrics.SalvarCSV(todasMedicoes, nomeArquivo); err != nil {
		log.Printf("ERRO ao salvar CSV: %v", err)
	} else {
		log.Printf("CSV salvo em: %s", nomeArquivo)
	}

	log.Printf("Benchmark serial concluido. Total de medições: %d", len(todasMedicoes))
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

// ─── Teste de multiplexing (concorrência) ────────────────────────────────────

// handlerIniciarConcorrencia dispara o benchmark de concorrência em background.
//
// Body JSON (todos opcionais):
//
//	{ "rodadas": 10, "aquecimento": 3 }
func handlerIniciarConcorrencia(cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "método não permitido", http.StatusMethodNotAllowed)
			return
		}

		estadoConc.mu.Lock()
		if estadoConc.rodando {
			estadoConc.mu.Unlock()
			responderJSON(w, http.StatusConflict, map[string]string{
				"erro": "benchmark de concorrência já em execução",
			})
			return
		}
		estadoConc.mu.Unlock()

		rodadas := 10
		aquecimento := 3

		var params struct {
			Rodadas     int `json:"rodadas"`
			Aquecimento int `json:"aquecimento"`
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			if err := json.Unmarshal(body, &params); err == nil {
				if params.Rodadas > 0 {
					rodadas = params.Rodadas
				}
				if params.Aquecimento >= 0 {
					aquecimento = params.Aquecimento
				}
			}
		}

		conn, err := grpc.NewClient(cfg.portaGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			responderJSON(w, http.StatusServiceUnavailable, map[string]string{
				"erro": fmt.Sprintf("falha ao conectar ao Carrinho Service: %v", err),
			})
			return
		}

		total := len(niveisConcorrencia) * rodadas * 2

		estadoConc.mu.Lock()
		estadoConc.rodando = true
		estadoConc.progresso = 0
		estadoConc.total = total
		estadoConc.medicoes = nil
		estadoConc.iniciadoEm = time.Now()
		estadoConc.erroFatal = ""
		estadoConc.mu.Unlock()

		go func() {
			defer conn.Close()
			executarBenchmarkConcorrencia(conn, cfg.baseHTTP, rodadas, aquecimento)
		}()

		responderJSON(w, http.StatusAccepted, map[string]interface{}{
			"mensagem":      "benchmark de concorrência iniciado",
			"rodadas":       rodadas,
			"aquecimento":   aquecimento,
			"concorrencias": niveisConcorrencia,
			"num_itens":     numItensConcorrencia,
			"total":         total,
			"status_url":    "/concorrencia/status",
			"resultado_url": "/concorrencia/resultados",
		})
	}
}

// handlerStatusConcorrencia retorna o progresso do benchmark de concorrência.
func handlerStatusConcorrencia(w http.ResponseWriter, r *http.Request) {
	estadoConc.mu.RLock()
	defer estadoConc.mu.RUnlock()

	pct := 0.0
	if estadoConc.total > 0 {
		pct = float64(estadoConc.progresso) / float64(estadoConc.total) * 100
	}
	responderJSON(w, http.StatusOK, map[string]interface{}{
		"rodando":    estadoConc.rodando,
		"progresso":  estadoConc.progresso,
		"total":      estadoConc.total,
		"percentual": fmt.Sprintf("%.1f%%", pct),
		"medicoes":   len(estadoConc.medicoes),
		"iniciado_em": estadoConc.iniciadoEm.Format(time.RFC3339),
		"erro":       estadoConc.erroFatal,
	})
}

// handlerResultadosConcorrencia devolve o CSV do teste de multiplexing.
func handlerResultadosConcorrencia(w http.ResponseWriter, r *http.Request) {
	estadoConc.mu.RLock()
	rodando := estadoConc.rodando
	medicoes := estadoConc.medicoes
	estadoConc.mu.RUnlock()

	if rodando {
		responderJSON(w, http.StatusPartialContent, map[string]string{
			"erro": "benchmark ainda em execução",
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
	w.Header().Set("Content-Disposition", `attachment; filename="concorrencia.csv"`)
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"protocolo", "concorrencia", "num_itens", "rodada",
		"throughput_rps", "latencia_media_us",
		"latencia_p50_us", "latencia_p95_us", "latencia_p99_us", "latencia_max_us",
		"erros", "timestamp",
	})
	for _, m := range medicoes {
		_ = cw.Write([]string{
			string(m.Protocolo),
			strconv.Itoa(m.Concorrencia),
			strconv.Itoa(m.NumItens),
			strconv.Itoa(m.Rodada),
			strconv.FormatFloat(m.Throughput, 'f', 3, 64),
			strconv.FormatInt(m.LatenciaMedia.Microseconds(), 10),
			strconv.FormatInt(m.LatenciaP50.Microseconds(), 10),
			strconv.FormatInt(m.LatenciaP95.Microseconds(), 10),
			strconv.FormatInt(m.LatenciaP99.Microseconds(), 10),
			strconv.FormatInt(m.LatenciaMax.Microseconds(), 10),
			strconv.Itoa(m.Erros),
			m.Timestamp.Format(time.RFC3339Nano),
		})
	}
}

// executarBenchmarkConcorrencia dispara N workers simultâneos para cada nível
// de concorrência e mede throughput e latências percentílicas.
func executarBenchmarkConcorrencia(conn *grpc.ClientConn, baseHTTP string, rodadas, aquecimento int) {
	clienteGRPC := pb.NewCarrinhoServiceClient(conn)
	// Cliente HTTP com pool de conexões grande o suficiente para não limitar o teste
	clienteHTTP := &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        200,
			MaxIdleConnsPerHost: 200,
		},
	}

	carrinhoID := fmt.Sprintf("cart-%d", numItensConcorrencia)
	var todasMedicoes []metrics.MedicaoConcorrencia

	for _, conc := range niveisConcorrencia {
		log.Printf("Testando concorrencia=%d...", conc)

		// Aquecimento
		for i := 0; i < aquecimento; i++ {
			_ = rodadaConcorrente(clienteGRPC, clienteHTTP, baseHTTP, carrinhoID, conc, -1)
		}

		// Rodadas registradas
		for r := 0; r < rodadas; r++ {
			pares := rodadaConcorrente(clienteGRPC, clienteHTTP, baseHTTP, carrinhoID, conc, r)
			todasMedicoes = append(todasMedicoes, pares...)

			estadoConc.mu.Lock()
			estadoConc.progresso += 2 // gRPC + JSON
			estadoConc.mu.Unlock()
		}
	}

	// Salva CSV em disco
	nomeArquivo := fmt.Sprintf("%s/concorrencia_%s.csv", pastaResultados, time.Now().Format("20060102_150405"))
	if err := metrics.SalvarCSVConcorrencia(todasMedicoes, nomeArquivo); err != nil {
		log.Printf("ERRO ao salvar CSV de concorrencia: %v", err)
	} else {
		log.Printf("CSV de concorrencia salvo em: %s", nomeArquivo)
	}

	estadoConc.mu.Lock()
	estadoConc.medicoes = todasMedicoes
	estadoConc.rodando = false
	estadoConc.mu.Unlock()

	log.Printf("Benchmark de concorrencia concluido. Total de medições: %d", len(todasMedicoes))
}

// rodadaConcorrente dispara `conc` workers simultaneamente para gRPC e JSON
// e retorna as métricas agregadas de cada protocolo.
func rodadaConcorrente(
	grpcClient pb.CarrinhoServiceClient,
	httpClient *http.Client,
	baseHTTP, carrinhoID string,
	conc, rodada int,
) []metrics.MedicaoConcorrencia {
	resultados := make([]metrics.MedicaoConcorrencia, 0, 2)
	for _, proto := range []metrics.Protocolo{metrics.GRPC, metrics.JSON} {
		m := dispararWorkers(grpcClient, httpClient, baseHTTP, carrinhoID, proto, conc, rodada)
		resultados = append(resultados, m)
	}
	return resultados
}

// dispararWorkers lança `conc` goroutines simultâneas para um protocolo,
// coleta as latências individuais e calcula as métricas agregadas.
func dispararWorkers(
	grpcClient pb.CarrinhoServiceClient,
	httpClient *http.Client,
	baseHTTP, carrinhoID string,
	proto metrics.Protocolo,
	conc, rodada int,
) metrics.MedicaoConcorrencia {
	latencias := make([]time.Duration, conc)
	erros := 0
	var wg sync.WaitGroup
	var mu sync.Mutex

	inicio := time.Now()
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var lat time.Duration
			var errStr string

			if proto == metrics.GRPC {
				m := benchGRPCObterCarrinho(grpcClient, carrinhoID, numItensConcorrencia, idx)
				lat = m.TempoTotal
				errStr = m.Erro
			} else {
				m := benchHTTPObterCarrinho(httpClient, baseHTTP, carrinhoID, numItensConcorrencia, idx)
				lat = m.TempoTotal
				errStr = m.Erro
			}

			mu.Lock()
			latencias[idx] = lat
			if errStr != "" {
				erros++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	duracao := time.Since(inicio)

	// Calcula percentis ordenando as latências
	ordenadas := make([]time.Duration, len(latencias))
	copy(ordenadas, latencias)
	ordenarDuracoes(ordenadas)

	n := len(ordenadas)
	var soma time.Duration
	for _, l := range ordenadas {
		soma += l
	}

	return metrics.MedicaoConcorrencia{
		Protocolo:     proto,
		Concorrencia:  conc,
		NumItens:      numItensConcorrencia,
		Rodada:        rodada,
		Throughput:    float64(conc) / duracao.Seconds(),
		LatenciaMedia: soma / time.Duration(n),
		LatenciaP50:   percentil(ordenadas, 50),
		LatenciaP95:   percentil(ordenadas, 95),
		LatenciaP99:   percentil(ordenadas, 99),
		LatenciaMax:   ordenadas[n-1],
		Erros:         erros,
		Timestamp:     time.Now(),
	}
}

// percentil retorna o percentil p (0-100) de uma fatia já ordenada.
func percentil(ordenadas []time.Duration, p int) time.Duration {
	if len(ordenadas) == 0 {
		return 0
	}
	idx := (len(ordenadas)-1)*p/100
	return ordenadas[idx]
}

// ordenarDuracoes ordena uma fatia de durations in-place (insertion sort — N pequeno).
func ordenarDuracoes(d []time.Duration) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
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

	log.Printf("Gateway Service iniciando na porta :%s", cfg.gatewayPort)
	log.Printf("   Carrinho Service: %s (gRPC) | %s (HTTP)", cfg.portaGRPC, cfg.baseHTTP)

	mux := http.NewServeMux()
	mux.HandleFunc("/benchmark/iniciar",        handlerIniciar(cfg))
	mux.HandleFunc("/benchmark/status",         handlerStatus)
	mux.HandleFunc("/benchmark/resultados",     handlerResultados)
	mux.HandleFunc("/concorrencia/iniciar",      handlerIniciarConcorrencia(cfg))
	mux.HandleFunc("/concorrencia/status",       handlerStatusConcorrencia)
	mux.HandleFunc("/concorrencia/resultados",   handlerResultadosConcorrencia)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		responderJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	log.Printf("   Endpoints disponíveis:")
	log.Printf("     POST http://localhost:%s/benchmark/iniciar", cfg.gatewayPort)
	log.Printf("     GET  http://localhost:%s/benchmark/status", cfg.gatewayPort)
	log.Printf("     GET  http://localhost:%s/benchmark/resultados", cfg.gatewayPort)
	log.Printf("     POST http://localhost:%s/concorrencia/iniciar", cfg.gatewayPort)
	log.Printf("     GET  http://localhost:%s/concorrencia/status", cfg.gatewayPort)
	log.Printf("     GET  http://localhost:%s/concorrencia/resultados", cfg.gatewayPort)

	if err := http.ListenAndServe(":"+cfg.gatewayPort, mux); err != nil {
		log.Fatalf("falha ao iniciar gateway: %v", err)
	}
}
