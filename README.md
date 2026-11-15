# Benchmark gRPC vs JSON — Serviço de Carrinho de Compras

**Trabalho GB — Sistemas Distribuídos — Unisinos 2026/2**

Medição comparativa dos benefícios do gRPC (Protocol Buffers) sobre REST/JSON
em um cenário de alta demanda com payloads grandes, usando um serviço de
carrinho de compras distribuído entre duas máquinas reais.

---

## Arquitetura

```
┌──────────────────────────────────────────────┐
│  macOS (sua máquina)                         │
│                                              │
│  ┌─────────────┐      ┌──────────────────┐  │
│  │  Você       │      │  Gateway Service │  │
│  │  curl /     │─HTTP─▶  localhost:3000  │  │
│  │  browser    │      │                  │  │
│  └─────────────┘      └────────┬─────────┘  │
│                                │             │
└────────────────────────────────┼─────────────┘
                                 │  gRPC :50051
                                 │  HTTP :8080
                            rede física
                                 │
┌────────────────────────────────┼─────────────┐
│  Omarchy (Arch Linux)          │             │
│                                ▼             │
│                 ┌──────────────────────┐     │
│                 │  Carrinho Service    │     │
│                 │  :50051 (gRPC)       │     │
│                 │  :8080  (HTTP/JSON)  │     │
│                 └──────────────────────┘     │
└──────────────────────────────────────────────┘
```

- **Carrinho Service**: lógica de negócio; responde às mesmas operações via gRPC e via HTTP/JSON
- **Gateway Service**: orquestra o benchmark; chama o Carrinho Service pelos dois protocolos, mede as métricas e expõe os resultados
- **Cliente**: faz requisições para o servições para o Gateway Service.

---

## Operações testadas

| Operação         | Descrição                                          |
|------------------|----------------------------------------------------|
| `AdicionarItem`  | Adiciona um produto ao carrinho                    |
| `ObterCarrinho`  | Retorna o carrinho completo                        |
| `Checkout`       | Submete o pedido, retorna resumo com todos itens   |

Tamanhos de carrinho: **1, 5, 10, 25, 50, 100, 250 e 500 itens**.

---

## Métricas coletadas

| Métrica                  | Como é medida                                              |
|--------------------------|------------------------------------------------------------|
| **Tamanho do payload**   | `len(bytes serializados)` — requisição + resposta          |
| **Tempo de serialização**| `time.Now()` antes/depois do marshal e unmarshal no Gateway|
| **Tempo de RTT**         | `time.Now()` antes da chamada até receber a resposta       |
| **Tempo de rede**        | RTT − serialização                                         |

---

## Estrutura do projeto

```
.
├── proto/
│   ├── cart.proto           # Tipos e serviço gRPC
│   └── cartpb/              # Código Go gerado pelo protoc
├── internal/
│   ├── factory/             # Gerador de dados sintéticos
│   └── metrics/             # Estruturas de métricas e exportação CSV
│
├── carrinho/
│   ├── main.go              # Servidor: gRPC :50051 + HTTP/JSON :8080
│   ├── Dockerfile
│   └── docker-compose.yml
│
├── gateway/
│   ├── main.go              # Serviço HTTP :3000 que orquestra o benchmark
│   ├── Dockerfile
│   └── docker-compose.yml
│
├── analysis/
│   ├── analisar.py          # Gera gráficos comparativos
│   └── requirements.txt
├── scripts/
│   └── rodar_benchmark.sh   # Script completo: inicia + polling + download CSV + gráficos
└── go.mod
```

---

## Pré-requisitos

### Carrinho Service

```bash
# Opção A: Go direto
sudo pacman -S go git
git clone <repo> && cd TrabalhoGB
go run ./carrinho/

# Opção B: Docker
sudo pacman -S docker git
sudo systemctl enable --now docker
git clone <repo> && cd TrabalhoGB/carrinho
sudo docker compose up -d
```

### Gateway Service

```bash
# Go
brew install go

# Dependências Python para gráficos
pip3 install -r analysis/requirements.txt
```

---

## Como executar

### 1. Descobrir o IP

No terminal:

```bash
ip addr show | grep "inet " | grep -v 127.0.0.1
```

### 2. Subir o Carrinho Service no Omarchy

**Com Go:**
```bash
cd TrabalhoGB
go run ./carrinho/
```

**Com Docker:**
```bash
cd TrabalhoGB/carrinho
sudo docker compose up -d
sudo docker compose logs -f
```

Verificar saúde:
```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

### 3. Subir o Gateway Service no macOS

```bash
cd TrabalhoGB
CARRINHO_HOST=192.168.1.42 go run ./gateway/
```

Saída esperada:
```
🚀 Gateway Service iniciando na porta :3000
   Carrinho Service: 192.168.1.42:50051 (gRPC) | http://192.168.1.42:8080 (HTTP)
   Endpoints disponíveis:
     POST http://localhost:3000/benchmark/iniciar
     GET  http://localhost:3000/benchmark/status
     GET  http://localhost:3000/benchmark/resultados
```

### 4. Executar o benchmark

**Opção A — script automático** (inicia + aguarda + baixa CSV + gera gráficos):
```bash
cd TrabalhoGB
./scripts/rodar_benchmark.sh 100 10
#                             ^   ^
#                    iterações   aquecimento
```

**Opção B — manualmente passo a passo:**

```bash
# Iniciar o benchmark (assíncrono)
curl -X POST http://localhost:3000/benchmark/iniciar \
  -H "Content-Type: application/json" \
  -d '{"iteracoes": 100, "aquecimento": 10}'

# Acompanhar o progresso
curl http://localhost:3000/benchmark/status

# Quando "rodando: false", baixar o CSV
curl http://localhost:3000/benchmark/resultados -o resultados.csv
```

### 5. Gerar os gráficos

```bash
python3 analysis/analisar.py resultados.csv
# Gráficos salvos em ./graficos/
```

---

## Endpoints do Gateway Service

| Método | Endpoint                    | Descrição                                      |
|--------|-----------------------------|------------------------------------------------|
| `POST` | `/benchmark/iniciar`        | Inicia benchmark; body JSON: `iteracoes`, `aquecimento` |
| `GET`  | `/benchmark/status`         | Progresso atual (`rodando`, `percentual`, etc.)|
| `GET`  | `/benchmark/resultados`     | Download do CSV (disponível após conclusão)    |
| `GET`  | `/health`                   | Health check                                   |

---

## Gráficos gerados

| Arquivo                      | Conteúdo                                                       |
|------------------------------|----------------------------------------------------------------|
| `1_payload_por_itens.png`    | Tamanho médio do payload (KB) vs número de itens              |
| `2_tempo_por_itens.png`      | RTT médio (ms) vs número de itens                             |
| `3_decomposicao_tempo.png`   | Barras empilhadas: tempo de rede + serialização                |
| `4_boxplot_rtt.png`          | Distribuição do RTT (4 tamanhos) — revela variância           |
| `5_fator_compressao.png`     | Razão JSON/gRPC por operação                                   |
| `6_heatmap_tempo.png`        | Heatmap RTT médio por operação × tamanho, por protocolo       |

---

## Regenerar o código Protobuf (se necessário)

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
brew install protobuf

protoc \
  --go_out=proto/cartpb --go_opt=paths=source_relative \
  --go-grpc_out=proto/cartpb --go-grpc_opt=paths=source_relative \
  --proto_path=proto \
  proto/cart.proto
```
