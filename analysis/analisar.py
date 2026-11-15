#!/usr/bin/env python3
"""
Análise e visualização dos resultados do benchmark gRPC vs JSON.
Gera gráficos comparativos salvos em PNG na pasta 'graficos/'.

Dependências:
    pip install pandas matplotlib seaborn

Uso:
    python3 analysis/analisar.py resultados.csv
"""

import sys
import os
import pandas as pd
import matplotlib.pyplot as plt
import matplotlib.ticker as ticker
import seaborn as sns
import numpy as np

# ─── Configuração visual ──────────────────────────────────────────────────────
sns.set_theme(style="whitegrid", palette="muted", font_scale=1.1)
CORES = {"gRPC": "#2196F3", "JSON": "#FF9800"}
PASTA_GRAFICOS = "graficos"

def carregar_dados(caminho: str) -> pd.DataFrame:
    df = pd.read_csv(caminho)
    # Remove medições com erro
    df = df[df["erro"].isna() | (df["erro"] == "")]
    # Converte unidades: µs → ms para tempo, bytes → KB para tamanho
    df["tempo_total_ms"]        = df["tempo_total_us"] / 1000.0
    df["tempo_serializacao_ms"] = df["tempo_serializacao_us"] / 1000.0
    df["tempo_rede_ms"]         = df["tempo_total_ms"] - df["tempo_serializacao_ms"]
    df["payload_kb"]            = df["tamanho_payload_bytes"] / 1024.0
    df["resposta_kb"]           = df["tamanho_resposta_bytes"] / 1024.0
    return df


def resumo_estatistico(df: pd.DataFrame):
    """Imprime tabela resumo no terminal."""
    print("\n" + "="*80)
    print("RESUMO ESTATÍSTICO — gRPC vs JSON")
    print("="*80)
    for op in df["operacao"].unique():
        sub = df[df["operacao"] == op]
        print(f"\n📦 Operação: {op}")
        print(f"{'Métrica':<35} {'gRPC':>12} {'JSON':>12} {'Redução':>10}")
        print("-" * 72)
        for metrica, col in [
            ("Payload médio (KB)",          "payload_kb"),
            ("Payload p95 (KB)",            "payload_kb"),
            ("Tempo total médio (ms)",      "tempo_total_ms"),
            ("Tempo total p95 (ms)",        "tempo_total_ms"),
            ("Tempo serialização médio (ms)","tempo_serializacao_ms"),
            ("Tempo de rede médio (ms)",    "tempo_rede_ms"),
        ]:
            agg = "mean" if "médio" in metrica else lambda x: x.quantile(0.95)
            grpc_val = sub[sub["protocolo"]=="gRPC"][col].agg(agg)
            json_val = sub[sub["protocolo"]=="JSON"][col].agg(agg)
            reducao  = (1 - grpc_val / json_val) * 100 if json_val != 0 else 0
            print(f"  {metrica:<33} {grpc_val:>12.3f} {json_val:>12.3f} {reducao:>9.1f}%")


# ─── Gráfico 1: Tamanho do payload por número de itens ───────────────────────
def grafico_payload_por_itens(df: pd.DataFrame, operacao: str = "ObterCarrinho"):
    sub = df[df["operacao"] == operacao].groupby(["protocolo", "num_itens"])["payload_kb"].mean().reset_index()

    fig, ax = plt.subplots(figsize=(10, 6))
    for proto, cor in CORES.items():
        dados = sub[sub["protocolo"] == proto]
        ax.plot(dados["num_itens"], dados["payload_kb"], marker="o", label=proto, color=cor, linewidth=2.5, markersize=6)

    ax.set_title(f"Tamanho do Payload vs Número de Itens\n(Operação: {operacao})", fontsize=14, fontweight="bold")
    ax.set_xlabel("Número de itens no carrinho (escala logarítmica)")
    ax.set_ylabel("Tamanho médio do payload (KB)")
    ax.set_xscale("log")
    ax.set_yscale("log")
    ax.xaxis.set_major_formatter(ticker.ScalarFormatter())
    ax.yaxis.set_major_formatter(ticker.ScalarFormatter())
    ax.legend(title="Protocolo")
    plt.tight_layout()
    salvar(fig, "1_payload_por_itens")


# ─── Gráfico 2: Tempo total médio por número de itens ────────────────────────
def grafico_tempo_por_itens(df: pd.DataFrame, operacao: str = "ObterCarrinho"):
    sub = df[df["operacao"] == operacao].groupby(["protocolo", "num_itens"])["tempo_total_ms"].mean().reset_index()

    fig, ax = plt.subplots(figsize=(10, 6))
    for proto, cor in CORES.items():
        dados = sub[sub["protocolo"] == proto]
        ax.plot(dados["num_itens"], dados["tempo_total_ms"], marker="s", label=proto, color=cor, linewidth=2.5, markersize=6)

    ax.set_title(f"Tempo de RTT Médio vs Número de Itens\n(Operação: {operacao})", fontsize=14, fontweight="bold")
    ax.set_xlabel("Número de itens no carrinho (escala logarítmica)")
    ax.set_ylabel("Tempo médio de RTT (ms)")
    ax.set_xscale("log")
    ax.set_yscale("log")
    ax.xaxis.set_major_formatter(ticker.ScalarFormatter())
    ax.yaxis.set_major_formatter(ticker.ScalarFormatter())
    ax.legend(title="Protocolo")
    plt.tight_layout()
    salvar(fig, "2_tempo_por_itens")


# ─── Gráfico 3: Decomposição de tempo (serialização vs rede) ─────────────────
def grafico_decomposicao_tempo(df: pd.DataFrame, operacao: str = "ObterCarrinho"):
    sub = df[df["operacao"] == operacao].groupby(["protocolo", "num_itens"])[
        ["tempo_serializacao_ms", "tempo_rede_ms"]
    ].mean().reset_index()

    protocolos = sub["protocolo"].unique()
    num_itens_vals = sorted(sub["num_itens"].unique())
    x = np.arange(len(num_itens_vals))
    largura = 0.35

    fig, ax = plt.subplots(figsize=(14, 7))
    offsets = {"gRPC": -largura/2, "JSON": largura/2}

    for proto in protocolos:
        dados = sub[sub["protocolo"] == proto].set_index("num_itens").reindex(num_itens_vals)
        cor_base = CORES[proto]
        ax.bar(x + offsets[proto], dados["tempo_rede_ms"],        largura, label=f"{proto} — Rede",          color=cor_base, alpha=0.9)
        ax.bar(x + offsets[proto], dados["tempo_serializacao_ms"],largura, label=f"{proto} — Serialização",  color=cor_base, alpha=0.45, bottom=dados["tempo_rede_ms"])

    ax.set_title(f"Decomposição do Tempo: Rede + Serialização\n(Operação: {operacao})", fontsize=14, fontweight="bold")
    ax.set_xlabel("Número de itens no carrinho")
    ax.set_ylabel("Tempo médio (ms, escala logarítmica)")
    ax.set_yscale("log")
    ax.set_xticks(x)
    ax.set_xticklabels([str(n) for n in num_itens_vals], rotation=45, ha="right")
    ax.legend(ncol=2)
    plt.tight_layout()
    salvar(fig, "3_decomposicao_tempo")


# ─── Gráfico 4: Box plot do tempo total por protocolo ────────────────────────
def grafico_boxplot_tempo(df: pd.DataFrame):
    # Usa os 4 maiores tamanhos presentes nos dados
    tamanhos_disponiveis = sorted(df["num_itens"].unique())
    tamanhos_foco = tamanhos_disponiveis[-4:]
    if len(tamanhos_foco) < 2:
        print("  ⚠️  Boxplot ignorado: dados insuficientes (mínimo 2 tamanhos)")
        return
    sub = df[df["num_itens"].isin(tamanhos_foco) & (df["operacao"] == "ObterCarrinho")]

    fig, axes = plt.subplots(1, len(tamanhos_foco), figsize=(16, 6), sharey=False)
    axes = np.atleast_1d(axes)
    for ax, n in zip(axes, tamanhos_foco):
        dados = sub[sub["num_itens"] == n]
        sns.boxplot(
            data=dados, x="protocolo", y="tempo_total_ms",
            hue="protocolo", palette=CORES, legend=False,
            ax=ax, width=0.5, fliersize=3,
        )
        ax.set_title(f"{n} itens")
        ax.set_xlabel("")
        ax.set_ylabel("RTT (ms)" if ax == axes[0] else "")


    fig.suptitle(
        "Distribuição do Tempo de RTT — gRPC vs JSON (ObterCarrinho)\n"
        "(eixo Y independente por tamanho)",
        fontsize=14, fontweight="bold",
    )
    plt.tight_layout()
    salvar(fig, "4_boxplot_rtt")

def grafico_fator_compressao(df: pd.DataFrame):
    fig, axes = plt.subplots(1, 2, figsize=(12, 6), sharey=True)

    for ax, op in zip(axes, ["ObterCarrinho", "Checkout"]):
        sub = df[df["operacao"] == op].groupby(["protocolo", "num_itens"])["payload_kb"].mean().unstack(level=0)
        if "gRPC" not in sub.columns or "JSON" not in sub.columns:
            continue
        fator = sub["JSON"] / sub["gRPC"]
        ax.bar(fator.index.astype(str), fator.values, color="#9C27B0", alpha=0.8)
        ax.axhline(1, color="red", linestyle="--", linewidth=1.5, label="Paridade (1×)")
        ax.set_title(op)
        ax.set_xlabel("Número de itens")
        ax.set_ylabel("Fator (JSON / gRPC)" if ax == axes[0] else "")
        ax.legend()

    fig.suptitle("Fator de Compressão: Payload JSON em relação ao gRPC\n(quanto maior, mais eficiente é o gRPC)", fontsize=13, fontweight="bold")
    plt.tight_layout()
    salvar(fig, "5_fator_compressao")


# ─── Gráfico 6: Heatmap de tempo médio por operação e tamanho ─────────────────
def grafico_heatmap(df: pd.DataFrame):
    fig, axes = plt.subplots(1, 2, figsize=(16, 6))

    for ax, proto in zip(axes, ["gRPC", "JSON"]):
        pivot = df[df["protocolo"] == proto].groupby(["operacao", "num_itens"])["tempo_total_ms"].mean().unstack()
        sns.heatmap(pivot, annot=True, fmt=".1f", cmap="YlOrRd", ax=ax, linewidths=0.5)
        ax.set_title(f"Tempo médio RTT (ms) — {proto}", fontweight="bold")
        ax.set_xlabel("Número de itens")
        ax.set_ylabel("Operação")

    plt.tight_layout()
    salvar(fig, "6_heatmap_tempo")


# ─── Utilitário ───────────────────────────────────────────────────────────────
# ─── Gráfico 7: Tempo de serialização vs número de itens (por operação) ──────
def grafico_serializacao_por_itens(df: pd.DataFrame):
    """
    Três subplots lado a lado — um por operação.
    Eixos X e Y em escala logarítmica para evidenciar como o custo de
    serialização cresce com o tamanho do payload em cada protocolo.
    """
    operacoes = ["ObterCarrinho", "Checkout"]
    fig, axes = plt.subplots(1, 2, figsize=(13, 6), sharey=False)

    for ax, op in zip(axes, operacoes):
        sub = (
            df[df["operacao"] == op]
            .groupby(["protocolo", "num_itens"])["tempo_serializacao_ms"]
            .mean()
            .reset_index()
        )
        for proto, cor in CORES.items():
            dados = sub[sub["protocolo"] == proto]
            ax.plot(
                dados["num_itens"],
                dados["tempo_serializacao_ms"],
                marker="o",
                label=proto,
                color=cor,
                linewidth=2.5,
                markersize=6,
            )
        ax.set_title(op, fontweight="bold")
        ax.set_xlabel("Número de itens")
        ax.set_ylabel("Tempo médio de serialização (ms)" if ax == axes[0] else "")
        ax.set_xscale("log")
        ax.set_yscale("log")
        ax.xaxis.set_major_formatter(ticker.ScalarFormatter())
        ax.yaxis.set_major_formatter(ticker.ScalarFormatter())
        ax.legend(title="Protocolo")

    fig.suptitle(
        "Tempo de Serialização vs Número de Itens\n"
        "(escala log-log — inclinação revela crescimento do custo)",
        fontsize=14,
        fontweight="bold",
    )
    plt.tight_layout()
    salvar(fig, "7_serializacao_por_itens")


def salvar(fig: plt.Figure, nome: str):
    os.makedirs(PASTA_GRAFICOS, exist_ok=True)
    caminho = os.path.join(PASTA_GRAFICOS, f"{nome}.png")
    fig.savefig(caminho, dpi=150, bbox_inches="tight")
    print(f"  💾 Salvo: {caminho}")
    plt.close(fig)


# ─── Main ─────────────────────────────────────────────────────────────────────
if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(f"Uso: python3 {sys.argv[0]} <caminho_resultados.csv>")
        sys.exit(1)

    caminho_csv = sys.argv[1]
    print(f"📊 Carregando dados de: {caminho_csv}")
    df = carregar_dados(caminho_csv)
    print(f"   {len(df)} medições válidas carregadas.")
    print(f"   Protocolos:  {df['protocolo'].unique().tolist()}")
    print(f"   Operações:   {df['operacao'].unique().tolist()}")
    print(f"   Tamanhos:    {sorted(df['num_itens'].unique().tolist())}")

    resumo_estatistico(df)

    print("\n📈 Gerando gráficos...")
    grafico_payload_por_itens(df)
    grafico_tempo_por_itens(df)
    grafico_decomposicao_tempo(df)
    grafico_boxplot_tempo(df)
    grafico_fator_compressao(df)
    grafico_heatmap(df)
    grafico_serializacao_por_itens(df)

    print(f"\n✅ Todos os gráficos salvos em './{PASTA_GRAFICOS}/'")
