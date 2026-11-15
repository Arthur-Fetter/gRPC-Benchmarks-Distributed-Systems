// Package metrics define as estruturas de métricas coletadas durante os benchmarks
// e funções utilitárias para exportar resultados em CSV.
package metrics

import (
	"encoding/csv"
	"fmt"
	"os"
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

// Medicao armazena todas as métricas de uma única chamada.
type Medicao struct {
	Protocolo         Protocolo
	Operacao          Operacao
	NumItens          int       // Número de itens no carrinho
	Iteracao          int       // Número da iteração (para estatísticas)
	TempoTotal        time.Duration // RTT completo (rede + serialização)
	TempoSerializacao time.Duration // Apenas marshal/unmarshal no cliente
	TamanhoPayload    int       // Bytes na requisição + resposta
	TamanhoRequisicao int       // Bytes apenas da requisição
	TamanhoResposta   int       // Bytes apenas da resposta
	Erro              string    // Vazio se sucesso
	Timestamp         time.Time
}

// SalvarCSV escreve uma lista de medições em um arquivo CSV.
func SalvarCSV(medicoes []Medicao, caminho string) error {
	f, err := os.Create(caminho)
	if err != nil {
		return fmt.Errorf("erro ao criar arquivo CSV: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Cabeçalho
	cabecalho := []string{
		"protocolo", "operacao", "num_itens", "iteracao",
		"tempo_total_us", "tempo_serializacao_us",
		"tamanho_payload_bytes", "tamanho_requisicao_bytes", "tamanho_resposta_bytes",
		"erro", "timestamp",
	}
	if err := w.Write(cabecalho); err != nil {
		return err
	}

	// Linhas
	for _, m := range medicoes {
		linha := []string{
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
		}
		if err := w.Write(linha); err != nil {
			return err
		}
	}

	return nil
}
