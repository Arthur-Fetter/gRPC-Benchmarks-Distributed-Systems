// Package factory gera dados sintéticos realistas para os testes de benchmark.
// Os dados são idênticos para gRPC e JSON, garantindo comparação justa.
package factory

import (
	"fmt"
	"math/rand"
	"time"

	pb "trabalho-gb/proto/cartpb"
)

var (
	categorias = []string{"Eletrônicos", "Roupas", "Alimentos", "Livros", "Esportes", "Casa", "Beleza", "Brinquedos"}
	marcas     = []string{"Samsung", "Apple", "Nike", "Adidas", "Sony", "LG", "Philips", "Bosch", "Natura", "Havaianas"}
	estados    = []string{"SP", "RJ", "MG", "RS", "PR", "SC", "BA", "PE", "CE", "GO"}
	cidades    = []string{"São Paulo", "Rio de Janeiro", "Belo Horizonte", "Porto Alegre", "Curitiba", "Florianópolis"}
	tiposDesc  = []string{"percentual", "fixo"}
)

// skuPrefixo retorna os primeiros 3 caracteres da marca, preenchendo com 'X' se necessário.
func skuPrefixo(marca string) string {
	if len(marca) >= 3 {
		return marca[:3]
	}
	return fmt.Sprintf("%-3s", marca) // ex: "LG" → "LG "
}

// NovoProduto cria um produto sintético com índice i para variação de dados.
func NovoProduto(i int) *pb.Produto {
	r := rand.New(rand.NewSource(int64(i)))
	return &pb.Produto{
		Id:         fmt.Sprintf("PROD-%06d", i),
		Nome:       fmt.Sprintf("Produto %d - %s", i, marcas[r.Intn(len(marcas))]),
		Descricao:  fmt.Sprintf("Descrição detalhada do produto número %d. Excelente qualidade, garantia de 12 meses, acompanha manual em português.", i),
		Categoria:  categorias[r.Intn(len(categorias))],
		Marca:      marcas[r.Intn(len(marcas))],
		Preco:      float64(r.Intn(50000)+100) / 100.0,
		PesoKg:     float64(r.Intn(500)+10) / 100.0,
		Estoque:    int32(r.Intn(1000) + 1),
		Sku:        fmt.Sprintf("SKU-%s-%06d", skuPrefixo(marcas[r.Intn(len(marcas))]), i),
		ImagemUrl:  fmt.Sprintf("https://cdn.loja.com.br/produtos/%06d/imagem-principal.jpg", i),
	}
}

// NovoDesconto cria um desconto sintético.
func NovoDesconto(i int) *pb.Desconto {
	r := rand.New(rand.NewSource(int64(i + 9999)))
	tipo := tiposDesc[r.Intn(len(tiposDesc))]
	valor := float64(r.Intn(30)+1) / 100.0
	if tipo == "fixo" {
		valor = float64(r.Intn(5000)+100) / 100.0
	}
	return &pb.Desconto{
		Codigo:    fmt.Sprintf("DESC-%04d", i),
		Tipo:      tipo,
		Valor:     valor,
		Descricao: fmt.Sprintf("Desconto especial de %s número %d", tipo, i),
	}
}

// NovoItem cria um item de carrinho com produto e descontos embutidos.
func NovoItem(i int) *pb.ItemCarrinho {
	r := rand.New(rand.NewSource(int64(i + 42)))
	produto := NovoProduto(i)
	qtd := int32(r.Intn(5) + 1)
	subtotal := produto.Preco * float64(qtd)

	// Cada item tem entre 0 e 2 descontos
	numDescontos := r.Intn(3)
	descontos := make([]*pb.Desconto, numDescontos)
	for d := 0; d < numDescontos; d++ {
		descontos[d] = NovoDesconto(i*10 + d)
		subtotal -= descontos[d].Valor
	}
	if subtotal < 0 {
		subtotal = produto.Preco * float64(qtd) * 0.9
	}

	return &pb.ItemCarrinho{
		Produto:    produto,
		Quantidade: qtd,
		Subtotal:   subtotal,
		Descontos:  descontos,
	}
}

// NovoEndereco cria um endereço de entrega sintético.
func NovoEndereco(seed int) *pb.EnderecoEntrega {
	r := rand.New(rand.NewSource(int64(seed)))
	return &pb.EnderecoEntrega{
		Rua:         fmt.Sprintf("Rua das Flores, %d", r.Intn(9000)+100),
		Numero:      fmt.Sprintf("%d", r.Intn(999)+1),
		Complemento: "Apto 42",
		Bairro:      "Centro",
		Cidade:      cidades[r.Intn(len(cidades))],
		Estado:      estados[r.Intn(len(estados))],
		Cep:         fmt.Sprintf("%05d-%03d", r.Intn(99999), r.Intn(999)),
		Pais:        "Brasil",
	}
}

// NovoCarrinho cria um carrinho completo com numItens itens.
func NovoCarrinho(carrinhoID, usuarioID string, numItens int) *pb.Carrinho {
	agora := time.Now()
	itens := make([]*pb.ItemCarrinho, numItens)
	totalBruto := 0.0
	totalDesconto := 0.0

	for i := 0; i < numItens; i++ {
		item := NovoItem(i)
		itens[i] = item
		bruto := item.Produto.Preco * float64(item.Quantidade)
		totalBruto += bruto
		totalDesconto += bruto - item.Subtotal
	}

	frete := 0.0
	if totalBruto < 299.0 {
		frete = 19.90
	}

	return &pb.Carrinho{
		Id:              carrinhoID,
		UsuarioId:       usuarioID,
		Itens:           itens,
		TotalBruto:      totalBruto,
		TotalDesconto:   totalDesconto,
		TotalLiquido:    totalBruto - totalDesconto + frete,
		Frete:           frete,
		Moeda:           "BRL",
		CriadoEm:        agora.Add(-10 * time.Minute).Format(time.RFC3339),
		AtualizadoEm:    agora.Format(time.RFC3339),
		EnderecoEntrega: NovoEndereco(42),
		Observacoes:     "Entregar no período da tarde, de preferência após as 14h.",
	}
}
