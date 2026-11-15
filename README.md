# Benchmark gRPC vs JSON — Serviço de Carrinho de Compras

**Trabalho GB — Sistemas Distribuídos — Unisinos 2026/2**

Medição comparativa dos benefícios do gRPC (Protocol Buffers) sobre REST/JSON
em um cenário de alta demanda com payloads grandes, usando um serviço de
carrinho de compras distribuído entre duas máquinas reais.

---

## Arquitetura

```
┌──────────────────────────────────────────────┐
│  macOS                                       │
│                                              │
│  ┌─────────────┐      ┌──────────────────┐  │
│  │  Cliente    │      │  Gateway Service │  │
│  │  curl /     │─HTTP─▶  localhost:3000  │  │
│  │  browser    │      │                  │  │
│  └─────────────┘      └────────┬─────────┘  │
│                                │             │
└────────────────────────────────┼─────────────┘
                                 │  gRPC :50051
                                 │  HTTP :8080
                            rede fisica
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

- **Carrinho Service** (Omarchy) — lógica de negócio; responde às mesmas operações via gRPC e via HTTP/JSON
- **Gateway Service** (macOS) — orquestra os benchmarks; chama o Carrinho Service pelos dois protocolos, mede as métricas e expõe os resultados via HTTP
- **Cliente** — curl ou browser; acessa o Gateway em localhost, sem atravessar a rede

---

## Operações testadas

| Operação         | Descrição                                        |
|------------------|--------------------------------------------------|
| `ObterCarrinho`  | Retorna o carrinho completo (N itens aninhados)  |
| `Checkout`       | Submete o pedido, retorna resumo com todos itens |

Tamanhos de carrinho testados: **1, 10, 100, 1000 e 10000 itens**.

---

## Benchmarks disponíveis

### Benchmark serial
Executa chamadas sequenciais para cada operação e tamanho de carrinho.
Mede tamanho de payload, tempo de serialização e RTT.

### Benchmark de concorrência
Dispara N workers simultaneamente para o mesmo endpoint.
Mede throughput (req/s) e latência percentílica sob carga.
Evidencia o benefício do HTTP/2 multiplexing do gRPC frente ao HTTP/1.1.

Níveis de concorrência testados: **1, 5, 10, 25, 50 e 100 workers**.

---

## Métricas coletadas

| Métrica                   | Como é medida                                               |
|---------------------------|-------------------------------------------------------------|
| **Tamanho do payload**    | Bytes serializados — requisição + resposta                  |
| **Tempo de serialização** | `time.Now()` antes/depois do marshal e unmarshal no Gateway |
| **Tempo de RTT**          | `time.Now()` antes da chamada até receber a resposta        |
| **Tempo de rede**         | RTT menos serialização                                      |
| **Throughput**            | Workers simultâneos dividido pela duração total da rodada   |
| **Latência percentílica** | p50, p95, p99 e máximo por rodada de concorrência           |

---

## Estrutura do projeto

```
.
├── proto/
│   ├── cart.proto           # Tipos e servico gRPC
│   └── cartpb/              # Codigo Go gerado pelo protoc
├── internal/
│   ├── factory/             # Gerador de dados sinteticos
│   └── metrics/             # Estruturas de metricas e exportacao CSV
│
├── carrinho/                # roda no OMARCHY
│   ├── main.go              # Servidor: gRPC :50051 + HTTP/JSON :8080
│   ├── Dockerfile
│   └── docker-compose.yml
│
├── gateway/                 # roda no MACOS
│   ├── main.go              # Servico HTTP :3000 que orquestra os benchmarks
│   ├── Dockerfile
│   └── docker-compose.yml
│
├── analysis/
│   ├── analisar.py          # Gera 9 graficos comparativos
│   └── requirements.txt
├── scripts/
│   └── rodar_benchmark.sh   # Script completo: serial + concorrencia + graficos
├── resultados/              # CSVs salvos automaticamente apos cada benchmark
└── go.mod
```

---

## Pré-requisitos

### Omarchy (Arch Linux) — Carrinho Service

```bash
sudo pacman -S go git
git clone <repo> && cd TrabalhoGB
go run ./carrinho/
```

Liberar as portas no firewall:

```bash
sudo ufw allow 8080/tcp
sudo ufw allow 50051/tcp
```

### macOS — Gateway Service

```bash
brew install go
pip3 install -r analysis/requirements.txt
```

---

## Como executar

### 1. Descobrir o IP do Omarchy

```bash
ip addr show | grep "inet " | grep -v 127.0.0.1
```

### 2. Subir o Carrinho Service no Omarchy

```bash
cd TrabalhoGB
go run ./carrinho/
```

Verificar:

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

### 3. Subir o Gateway Service no macOS

```bash
cd TrabalhoGB
CARRINHO_HOST=<IP_DO_OMARCHY> go run ./gateway/
```

### 4. Executar os benchmarks

**Script automatico** — roda serial + concorrencia + gera todos os graficos:

```bash
./scripts/rodar_benchmark.sh [iteracoes] [aquecimento] [rodadas_concorrencia]

# Exemplo
./scripts/rodar_benchmark.sh 100 10 20
```

**Manual — benchmark serial:**

```bash
# Inicia
curl -X POST http://localhost:3000/benchmark/iniciar \
  -H "Content-Type: application/json" \
  -d '{"iteracoes": 100, "aquecimento": 10}'

# Acompanha
curl http://localhost:3000/benchmark/status

# Baixa o CSV
curl http://localhost:3000/benchmark/resultados -o resultados/serial.csv
```

**Manual — benchmark de concorrencia:**

```bash
# Inicia
curl -X POST http://localhost:3000/concorrencia/iniciar \
  -H "Content-Type: application/json" \
  -d '{"rodadas": 20, "aquecimento": 3}'

# Acompanha
curl http://localhost:3000/concorrencia/status

# Baixa o CSV
curl http://localhost:3000/concorrencia/resultados -o resultados/concorrencia.csv
```

### 5. Gerar os graficos

```bash
# Passa os dois CSVs — o de concorrencia e opcional
python3 analysis/analisar.py resultados/serial.csv resultados/concorrencia.csv

# Sem o CSV de concorrencia, gera apenas os graficos seriais
python3 analysis/analisar.py resultados/serial.csv
```

Os CSVs tambem sao salvos automaticamente em `resultados/` com timestamp ao fim de cada benchmark.

---

## Endpoints do Gateway Service

| Metodo | Endpoint                      | Descricao                                            |
|--------|-------------------------------|------------------------------------------------------|
| `POST` | `/benchmark/iniciar`          | Inicia benchmark serial; parametros: `iteracoes`, `aquecimento` |
| `GET`  | `/benchmark/status`           | Progresso do benchmark serial                        |
| `GET`  | `/benchmark/resultados`       | Download do CSV serial                               |
| `POST` | `/concorrencia/iniciar`       | Inicia benchmark de concorrencia; parametros: `rodadas`, `aquecimento` |
| `GET`  | `/concorrencia/status`        | Progresso do benchmark de concorrencia               |
| `GET`  | `/concorrencia/resultados`    | Download do CSV de concorrencia                      |
| `GET`  | `/health`                     | Health check                                         |

---

## Graficos gerados

| Arquivo                        | Conteudo                                                    |
|--------------------------------|-------------------------------------------------------------|
| `1_payload_por_itens.png`      | Tamanho medio do payload (KB) por numero de itens           |
| `2_tempo_por_itens.png`        | RTT medio (ms) por numero de itens                          |
| `3_decomposicao_tempo.png`     | Tempo de rede e serializacao empilhados por tamanho         |
| `4_boxplot_rtt.png`            | Distribuicao do RTT nos 4 maiores tamanhos                  |
| `5_fator_compressao.png`       | Razao de tamanho JSON / gRPC por operacao                   |
| `6_heatmap_tempo.png`          | Heatmap de RTT medio por operacao e tamanho                 |
| `7_serializacao_por_itens.png` | Tempo de serializacao por numero de itens                   |
| `8_throughput_concorrencia.png`| Throughput (req/s) por nivel de concorrencia                |
| `9_latencia_concorrencia.png`  | Latencia media e p99 por nivel de concorrencia              |

---

## Regenerar o codigo Protobuf

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
