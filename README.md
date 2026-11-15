# Benchmark gRPC vs JSON — Serviço de Carrinho de Compras

**Trabalho GB Sistemas Distribuídos**

Medição comparativa dos benefícios do gRPC (Protocol Buffers) sobre REST/JSON em um cenário de alta demanda com payloads grandes, usando um serviço de carrinho de compras distribuído entre duas máquinas reais.

---

## Arquitetura

```
┌───────────────────────────────────────────┐
│                                           │
│  ┌───────────┐      ┌──────────────────┐  │
│  │           │      │                  │  │
│  │  Cliente  │─HTTP─▶  Gateway Service │  │
│  │           │      │                  │  │
│  └───────────┘      └────────┬─────────┘  │
│                              │            │
└──────────────────────────────┼────────────┘
                               │  gRPC :50051
                               │  HTTP :8080
                          rede física
                               │
┌──────────────────────────────┼─────────────┐
│                              │             │
│                              ▼             │
│               ┌──────────────────────┐     │
│               │  Carrinho Service    │     │
│               │  :50051 (gRPC)       │     │
│               │  :8080  (HTTP/JSON)  │     │
│               └──────────────────────┘     │
└────────────────────────────────────────────┘
```

- **Carrinho Service**: lógica de negócio; responde às mesmas operações via gRPC e via HTTP/JSON
- **Gateway Service**: orquestra o benchmark; chama o Carrinho Service pelos dois protocolos, mede as métricas e expõe os resultados
- **Cliente**: faz requisições para o servições para o Gateway Service.

---

## Operações testadas

| Operação         | Descrição                                        |
|------------------|--------------------------------------------------|
| `ObterCarrinho`  | Retorna o carrinho completo (N itens aninhados)  |
| `Checkout`       | Submete o pedido, retorna resumo com todos itens |

Tamanhos de carrinho testados: **1, 10, 100, 1000 e 10000 itens**.
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

## Benchmarks disponíveis

### Benchmark serial
Execução sequencial de chamadas para cada operação e tamanho de carrinho.
Mede tamanho de payload, tempo de serialização e RTT.

### Benchmark de concorrência
Dispara N workers simultaneamente para o mesmo endpoint.
Mede throughput (req/s) e latência percentílica sob carga.
Evidencia o benefício do HTTP/2 multiplexing do gRPC frente ao HTTP/1.1.

Níveis de concorrência testados: **1, 5, 10, 25, 50 e 100 workers**.

---

## Como executar
### 1. Subir o Carrinho Service no Omarchy

**Com Go:**
```bash
go run ./carrinho/
```
Verificar saúde:
```bash
curl http://localhost:8080/health
```

### 2. Subir o Gateway Service no macOS

```bash
CARRINHO_HOST=<ip servidor> go run ./gateway/
```

Saída esperada:
```
   Gateway Service iniciando na porta :3000
   Carrinho Service: <ip servidor>:50051 (gRPC) | http://192.168.1.42:8080 (HTTP)
   Endpoints disponíveis:
     POST http://localhost:3000/benchmark/iniciar
     GET  http://localhost:3000/benchmark/status
     GET  http://localhost:3000/benchmark/resultados
```

### 3. Executar os benchmarks

**Script automático** — roda serial + concorrência + gera todos os gráficos:

```bash
./scripts/rodar_benchmark.sh [iteracoes] [aquecimento] [rodadas_concorrencia]

# Exemplo
./scripts/rodar_benchmark.sh 100 10 20
```

**Manual — benchmark serial:**

```bash
curl -X POST http://localhost:3000/benchmark/iniciar \
  -H "Content-Type: application/json" \
  -d '{"iteracoes": 100, "aquecimento": 10}'

curl http://localhost:3000/benchmark/status

curl http://localhost:3000/benchmark/resultados -o resultados/serial.csv
```

**Manual — benchmark de concorrência:**

```bash
curl -X POST http://localhost:3000/concorrencia/iniciar \
  -H "Content-Type: application/json" \
  -d '{"rodadas": 20, "aquecimento": 3}'

curl http://localhost:3000/concorrencia/status

curl http://localhost:3000/concorrencia/resultados -o resultados/concorrencia.csv
```

### 4. Gerar os gráficos

```bash
# Passa os dois CSVs
python3 analysis/analisar.py resultados/serial.csv resultados/concorrencia.csv

# Sem o CSV de concorrência, gera apenas os gráficos seriais
python3 analysis/analisar.py resultados/serial.csv
```

Os CSVs também são salvos automaticamente em `resultados/` com timestamp ao fim de cada benchmark.

---

## Endpoints do Gateway Service

| Método | Endpoint                    | Descrição                                                        |
|--------|-----------------------------|-----------------------------------------------------------------|
| `POST` | `/benchmark/iniciar`        | Inicia benchmark serial; parâmetros: `iteracoes`, `aquecimento` |
| `GET`  | `/benchmark/status`         | Progresso do benchmark serial                                   |
| `GET`  | `/benchmark/resultados`     | Download do CSV serial                                          |
| `POST` | `/concorrencia/iniciar`     | Inicia benchmark de concorrência; parâmetros: `rodadas`, `aquecimento` |
| `GET`  | `/concorrencia/status`      | Progresso do benchmark de concorrência                          |
| `GET`  | `/concorrencia/resultados`  | Download do CSV de concorrência                                 |
| `GET`  | `/health`                   | Health check                                                    |

---

## Gráficos gerados

| Arquivo                         | Conteúdo                                                  |
|---------------------------------|-----------------------------------------------------------|
| `1_payload_por_itens.png`       | Tamanho médio do payload (KB) por número de itens         |
| `2_tempo_por_itens.png`         | RTT médio (ms) por número de itens                        |
| `3_decomposicao_tempo.png`      | Tempo de rede e serialização empilhados por tamanho       |
| `4_boxplot_rtt.png`             | Distribuição do RTT nos 4 maiores tamanhos                |
| `5_fator_compressao.png`        | Razão de tamanho JSON / gRPC por operação                 |
| `6_heatmap_tempo.png`           | Heatmap de RTT médio por operação e tamanho               |
| `7_serializacao_por_itens.png`  | Tempo de serialização por número de itens                 |
| `8_throughput_concorrencia.png` | Throughput (req/s) por nível de concorrência              |
| `9_latencia_concorrencia.png`   | Latência média e p99 por nível de concorrência            |

