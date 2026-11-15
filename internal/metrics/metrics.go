// Package metrics define as estruturas de métricas coletadas durante os benchmarks
// e funções utilitárias para exportar resultados em CSV.
package metrics

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Protocolo representa o tipo de comunicação utilizado.
type Protocolo string

const (
	GRPC Protocolo = "gRPC"
	JSON Protocolo = "JSON"
)

// Operacao representa qual RPC/endpoint foi chamado.
type Operacao string

const (
	OpAdicionarItem Operacao = "AdicionarItem"
	OpObterCarrinho Operacao = "ObterCarrinho"
	OpCheckout      Operacao = "Checkout"
)

// Medicao armazena todas as métricas de uma única chamada serial.
type Medicao struct {
	Protocolo         Protocolo
	Operacao          Operacao
	NumItens          int
	Iteracao          int
	TempoTotal        time.Duration
	TempoSerializacao time.Duration
	TamanhoPayload    int
	TamanhoRequisicao int
	TamanhoResposta   int
	Erro              string
	Timestamp         time.Time
}

// MedicaoConcorrencia armazena o resultado de uma rodada do teste de multiplexing.
// Cada rodada dispara Concorrencia chamadas simultâneas e mede o comportamento agregado.
type MedicaoConcorrencia struct {
	Protocolo    Protocolo
	Concorrencia int           // número de workers simultâneos
	NumItens     int           // itens por carrinho (fixo em cada rodada)
	Rodada       int           // índice da repetição
	Throughput   float64       // requisições por segundo (concorrencia / duracao_total)
	LatenciaMedia time.Duration // média de latência individual dos workers
	LatenciaP50  time.Duration
	LatenciaP95  time.Duration
	LatenciaP99  time.Duration
	LatenciaMax  time.Duration
	Erros        int           // workers que falharam
	Timestamp    time.Time
}

// SalvarCSV escreve uma lista de medições seriais em um arquivo CSV.
func SalvarCSV(medicoes []Medicao, caminho string) error {
	if err := os.MkdirAll(filepath.Dir(caminho), 0755); err != nil {
		return fmt.Errorf("erro ao criar diretório: %w", err)
	}
	f, err := os.Create(caminho)
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo CSV: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	_ = w.Write([]string{
		"protocolo", "operacao", "num_itens", "iteracao",
		"tempo_total_us", "tempo_serializacao_us",
		"tamanho_payload_bytes", "tamanho_requisicao_bytes", "tamanho_resposta_bytes",
		"erro", "timestamp",
	})
	for _, m := range medicoes {
		_ = w.Write([]string{
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
	return nil
}

// SalvarCSVConcorrencia escreve os resultados do teste de multiplexing em CSV.
func SalvarCSVConcorrencia(medicoes []MedicaoConcorrencia, caminho string) error {
	if err := os.MkdirAll(filepath.Dir(caminho), 0755); err != nil {
		return fmt.Errorf("erro ao criar diretório: %w", err)
	}
	f, err := os.Create(caminho)
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo CSV: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	_ = w.Write([]string{
		"protocolo", "concorrencia", "num_itens", "rodada",
		"throughput_rps", "latencia_media_us",
		"latencia_p50_us", "latencia_p95_us", "latencia_p99_us", "latencia_max_us",
		"erros", "timestamp",
	})
	for _, m := range medicoes {
		_ = w.Write([]string{
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
	return nil
}
