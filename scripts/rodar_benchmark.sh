#!/usr/bin/env bash
# scripts/rodar_benchmark.sh
# Dispara benchmark serial + benchmark de concorrência, baixa os CSVs e gera gráficos.
#
# Uso: ./scripts/rodar_benchmark.sh [iteracoes] [aquecimento] [rodadas_conc]

set -euo pipefail

ITERACOES="${1:-100}"
AQUECIMENTO="${2:-10}"
RODADAS_CONC="${3:-20}"
GATEWAY="http://localhost:3000"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
CSV_SERIAL="resultados/serial_${TIMESTAMP}.csv"
CSV_CONC="resultados/concorrencia_${TIMESTAMP}.csv"
mkdir -p resultados

echo "=================================================="
echo "  Benchmark gRPC vs JSON - Sistemas Distribuidos"
echo "=================================================="
echo "  Gateway:         $GATEWAY"
echo "  Iteracoes:       $ITERACOES (+$AQUECIMENTO aquecimento)"
echo "  Rodadas conc.:   $RODADAS_CONC"
echo "=================================================="

# Verifica gateway
echo -n "  Verificando gateway... "
if curl -sf "$GATEWAY/health" > /dev/null; then
    echo "OK"
else
    echo "FALHOU"
    echo "  ERRO: Gateway nao acessivel. Inicie com: CARRINHO_HOST=<IP> go run ./gateway/"
    exit 1
fi

# ── Benchmark serial ──────────────────────────────────────────────────────────
echo ""
echo "  [1/2] Iniciando benchmark serial..."
curl -sf -X POST "$GATEWAY/benchmark/iniciar" \
    -H "Content-Type: application/json" \
    -d "{\"iteracoes\": $ITERACOES, \"aquecimento\": $AQUECIMENTO}" > /dev/null

echo -n "  Progresso: "
while true; do
    STATUS=$(curl -sf "$GATEWAY/benchmark/status")
    RODANDO=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['rodando'])")
    PCT=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['percentual'])")
    echo -ne "\r  Progresso: $PCT    "
    [ "$RODANDO" = "False" ] && break
    sleep 3
done
echo ""
curl -sf "$GATEWAY/benchmark/resultados" -o "$CSV_SERIAL"
echo "  CSV serial salvo: $CSV_SERIAL"

# ── Benchmark de concorrência ─────────────────────────────────────────────────
echo ""
echo "  [2/2] Iniciando benchmark de concorrencia..."
curl -sf -X POST "$GATEWAY/concorrencia/iniciar" \
    -H "Content-Type: application/json" \
    -d "{\"rodadas\": $RODADAS_CONC, \"aquecimento\": 3}" > /dev/null

echo -n "  Progresso: "
while true; do
    STATUS=$(curl -sf "$GATEWAY/concorrencia/status")
    RODANDO=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['rodando'])")
    PCT=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['percentual'])")
    echo -ne "\r  Progresso: $PCT    "
    [ "$RODANDO" = "False" ] && break
    sleep 3
done
echo ""
curl -sf "$GATEWAY/concorrencia/resultados" -o "$CSV_CONC"
echo "  CSV concorrencia salvo: $CSV_CONC"

# ── Gráficos ──────────────────────────────────────────────────────────────────
echo ""
echo "  Gerando graficos..."
python3 analysis/analisar.py "$CSV_SERIAL" "$CSV_CONC"

echo ""
echo "  Benchmark concluido!"
echo "  Serial:      $CSV_SERIAL"
echo "  Concorrencia: $CSV_CONC"
echo "  Graficos:    ./graficos/"
