// Servidor principal do Trabalho GB - Sistemas Distribuídos
// Expõe dois endpoints com a mesma lógica de negócio:
//   - gRPC na porta 50051
//   - HTTP/JSON na porta 8080
//
// Executar: go run ./server/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"trabalho-gb/internal/factory"
	pb "trabalho-gb/proto/cartpb"
)

// ─── Implementação gRPC ──────────────────────────────────────────────────────

type servidorGRPC struct {
	pb.UnimplementedCarrinhoServiceServer
}

// AdicionarItem recebe um pedido de adição de item e retorna o carrinho atualizado.
// Para o benchmark, o carrinho é gerado sinteticamente com base no produto_id.
func (s *servidorGRPC) AdicionarItem(_ context.Context, req *pb.AdicionarItemRequest) (*pb.AdicionarItemResponse, error) {
	// Simula lógica de negócio: busca carrinho existente ou cria novo com 1 item
	carrinho := factory.NovoCarrinho(req.CarrinhoId, "usuario-001", 1)
	return &pb.AdicionarItemResponse{
		Sucesso:  true,
		Mensagem: "Item adicionado com sucesso",
		Carrinho: carrinho,
	}, nil
}

// ObterCarrinho retorna o carrinho completo. O número de itens é codificado
// nos primeiros caracteres do carrinho_id para fins de benchmark (ex: "cart-050").
func (s *servidorGRPC) ObterCarrinho(_ context.Context, req *pb.ObterCarrinhoRequest) (*pb.ObterCarrinhoResponse, error) {
	numItens := extrairNumItens(req.CarrinhoId)
	carrinho := factory.NovoCarrinho(req.CarrinhoId, "usuario-001", numItens)
	return &pb.ObterCarrinhoResponse{Carrinho: carrinho}, nil
}

// Checkout processa o pedido e retorna um resumo do pedido criado.
func (s *servidorGRPC) Checkout(_ context.Context, req *pb.CheckoutRequest) (*pb.CheckoutResponse, error) {
	numItens := extrairNumItens(req.CarrinhoId)
	carrinho := factory.NovoCarrinho(req.CarrinhoId, "usuario-001", numItens)

	pedido := &pb.Pedido{
		Id:              fmt.Sprintf("PEDIDO-%d", time.Now().UnixNano()),
		CarrinhoId:      req.CarrinhoId,
		UsuarioId:       "usuario-001",
		Status:          "confirmado",
		Total:           carrinho.TotalLiquido,
		MetodoPagamento: req.MetodoPagamento,
		CriadoEm:        time.Now().Format(time.RFC3339),
		Endereco:        req.EnderecoEntrega,
		Itens:           carrinho.Itens,
	}
	return &pb.CheckoutResponse{
		Sucesso:  true,
		Mensagem: "Pedido realizado com sucesso",
		Pedido:   pedido,
	}, nil
}

// ─── Implementação HTTP/JSON ─────────────────────────────────────────────────

// Usamos as mesmas structs do protobuf convertidas para JSON via encoding/json.
// O servidor HTTP recebe o body JSON, deserializa, executa a mesma lógica,
// serializa a resposta e envia de volta.

func handlerAdicionarItem(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "erro ao ler body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		CarrinhoID string `json:"carrinho_id"`
		ProdutoID  string `json:"produto_id"`
		Quantidade int32  `json:"quantidade"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "erro ao deserializar JSON", http.StatusBadRequest)
		return
	}

	carrinho := factory.NovoCarrinho(req.CarrinhoID, "usuario-001", 1)
	resp := map[string]interface{}{
		"sucesso":  true,
		"mensagem": "Item adicionado com sucesso",
		"carrinho": carrinho,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handlerObterCarrinho(w http.ResponseWriter, r *http.Request) {
	carrinhoID := r.URL.Query().Get("carrinho_id")
	numItens := extrairNumItens(carrinhoID)
	carrinho := factory.NovoCarrinho(carrinhoID, "usuario-001", numItens)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"carrinho": carrinho,
	})
}

func handlerCheckout(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "erro ao ler body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		CarrinhoID      string              `json:"carrinho_id"`
		EnderecoEntrega *pb.EnderecoEntrega `json:"endereco_entrega"`
		MetodoPagamento string              `json:"metodo_pagamento"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "erro ao deserializar JSON", http.StatusBadRequest)
		return
	}

	numItens := extrairNumItens(req.CarrinhoID)
	carrinho := factory.NovoCarrinho(req.CarrinhoID, "usuario-001", numItens)

	pedido := &pb.Pedido{
		Id:              fmt.Sprintf("PEDIDO-%d", time.Now().UnixNano()),
		CarrinhoId:      req.CarrinhoID,
		UsuarioId:       "usuario-001",
		Status:          "confirmado",
		Total:           carrinho.TotalLiquido,
		MetodoPagamento: req.MetodoPagamento,
		CriadoEm:        time.Now().Format(time.RFC3339),
		Endereco:        req.EnderecoEntrega,
		Itens:           carrinho.Itens,
	}
	resp := map[string]interface{}{
		"sucesso":  true,
		"mensagem": "Pedido realizado com sucesso",
		"pedido":   pedido,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ─── Utilitários ─────────────────────────────────────────────────────────────

// extrairNumItens lê o número de itens codificado no carrinho_id.
// Formato esperado: "cart-NNN" onde NNN é o número de itens.
// Retorna 10 como padrão se o formato não for reconhecido.
func extrairNumItens(carrinhoID string) int {
	var n int
	_, err := fmt.Sscanf(carrinhoID, "cart-%d", &n)
	if err != nil || n <= 0 {
		return 10
	}
	return n
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	portaGRPC := "50051"
	portaHTTP := "8080"
	if p := os.Getenv("GRPC_PORT"); p != "" {
		portaGRPC = p
	}
	if p := os.Getenv("HTTP_PORT"); p != "" {
		portaHTTP = p
	}

	// Inicia servidor gRPC em goroutine separada
	go func() {
		lis, err := net.Listen("tcp", ":"+portaGRPC)
		if err != nil {
			log.Fatalf("falha ao escutar na porta gRPC %s: %v", portaGRPC, err)
		}
		s := grpc.NewServer()
		pb.RegisterCarrinhoServiceServer(s, &servidorGRPC{})
		reflection.Register(s) // permite inspeção com grpcurl
		log.Printf("Servidor gRPC escutando em :%s", portaGRPC)
		if err := s.Serve(lis); err != nil {
			log.Fatalf("falha ao servir gRPC: %v", err)
		}
	}()

	// Inicia servidor HTTP/JSON
	mux := http.NewServeMux()
	mux.HandleFunc("/carrinho/adicionar", handlerAdicionarItem)
	mux.HandleFunc("/carrinho/obter", handlerObterCarrinho)
	mux.HandleFunc("/carrinho/checkout", handlerCheckout)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("Servidor HTTP/JSON escutando em :%s", portaHTTP)
	if err := http.ListenAndServe(":"+portaHTTP, mux); err != nil {
		log.Fatalf("falha ao servir HTTP: %v", err)
	}
}
