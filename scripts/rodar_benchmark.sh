#!/usr/bin/env bash
# scripts/rodar_benchmark.sh
# Dispara o benchmark via Gateway Service e baixa o CSV ao terminar.
#
# Pré-requisito: Gateway Service rodando em localhost:3000
#   CARRINHO_HOST=192.168.1.42 go run ./gateway/
#
# Uso: ./scripts/rodar_benchmark.sh [iteracoes] [aquecimento]

set -euo pipefail

ITERACOES="${1:-100}"
AQUECIMENTO="${2:-10}"
GATEWAY="http://localhost:3000"
SAIDA="resultados_$(date +%Y%m%d_%H%M%S).csv"

echo "=================================================="
echo "  Benchmark gRPC vs JSON - Sistemas Distribuidos"
echo "=================================================="
echo "  Gateway:     $GATEWAY"
echo "  Iteracoes:   $ITERACOES (+$AQUECIMENTO aquecimento)"
echo "  Saida:       $SAIDA"
echo "=================================================="

# Verifica se o gateway está no ar
echo -n "  Verificando gateway... "
if curl -sf "$GATEWAY/health" > /dev/null; then
    echo "OK"
else
    echo "FALHOU"
    echo "  ERRO: Gateway nao acessivel em $GATEWAY"
    echo "  Inicie com: CARRINHO_HOST=<IP> go run ./gateway/"
    exit 1
fi

# Inicia o benchmark
echo ""
echo "  Iniciando benchmark..."
curl -sf -X POST "$GATEWAY/benchmark/iniciar" \
    -H "Content-Type: application/json" \
    -d "{\"iteracoes\": $ITERACOES, \"aquecimento\": $AQUECIMENTO}" | \
    python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  Total de medicoes previstas: {d[\"total\"]}')"

# Aguarda conclusao com polling
echo ""
echo -n "  Progresso: "
while true; do
    STATUS=$(curl -sf "$GATEWAY/benchmark/status")
    RODANDO=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['rodando'])")
    PCT=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['percentual'])")
    echo -ne "\r  Progresso: $PCT    "
    if [ "$RODANDO" = "False" ]; then
        break
    fi
    sleep 2
done
echo ""

# Baixa o CSV
echo "  Baixando resultados..."
curl -sf "$GATEWAY/benchmark/resultados" -o "$SAIDA"
echo "  CSV salvo em: $SAIDA"

# Gera graficos
echo ""
echo "  Gerando graficos..."
python3 analysis/analisar.py "$SAIDA"

echo ""
echo "  Benchmark concluido!"
echo "  CSV:      $SAIDA"
echo "  Graficos: ./graficos/"
