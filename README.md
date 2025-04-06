# Documentação do Sistema de Transações Bancárias em Go

Esta documentação explica o código de um sistema de API bancária desenvolvido em Go para processar transações financeiras e fornecer informações de extrato para clientes.

## Visão Geral

O sistema é uma API REST que permite realizar operações bancárias básicas:
- Registrar transações (débitos e créditos) para clientes
- Consultar o extrato bancário de um cliente, incluindo saldo atual e histórico de transações recentes

A aplicação utiliza MySQL como banco de dados e é construída seguindo práticas de desenvolvimento robustas, incluindo tratamento de erros, validações e otimizações de desempenho.

## Estrutura do Código

### Modelos de Dados

```go
type Statement struct {
    Saldo             Saldo        `json:"saldo"`
    UltimasTransacoes []Transacoes `json:"ultimas_transacoes"`
}

type Saldo struct {
    Total       int64     `json:"total"`
    DataExtrato time.Time `json:"data_extrato"`
    Limite      int64     `json:"limite"`
}

type Transacoes struct {
    Valor       int64     `json:"valor"`
    Tipo        string    `json:"tipo"`
    Descricao   string    `json:"descricao"`
    RealizadaEm time.Time `json:"realizada_em"`
}

type TransactionRequest struct {
    Valor     int64  `json:"valor"`
    Tipo      string `json:"tipo"`
    Descricao string `json:"descricao"`
}

type TransactionSuccessResponse struct {
    Limite int64 `json:"limite"`
    Saldo  int64 `json:"saldo"`
}
```

### Constantes e Variáveis Globais

```go
var serverPort = os.Getenv("API_PORT")

const (
    maxDBIdleConns    = 5
    maxDBOpenConns    = 10
    dbConnMaxLifetime = 5 * time.Minute
    minClientID       = 1
    maxClientID       = 5
    maxDescricaoLen   = 10
    maxTransactions   = 10
)

var (
    ErrLimiteExcedido    = errors.New("limite excedido")
    ErrClienteInvalido   = errors.New("cliente inválido")
    ErrValorInvalido     = errors.New("valor inválido")
    ErrTipoInvalido      = errors.New("tipo inválido")
    ErrDescricaoInvalida = errors.New("descrição inválida")
)
```

## Principais Funcionalidades

### 1. Conexão com o Banco de Dados

A função `conectarBanco()` estabelece uma conexão com o MySQL com suporte a retentativas, configurando parâmetros de performance como número máximo de conexões e timeout.

### 2. Processamento de Transações

A função `salvarTransacao()` implementa a lógica de negócio para registrar uma transação:

1. Inicia uma transação de banco de dados
2. Obtém o saldo atual e limite do cliente usando lock (FOR UPDATE) para evitar condições de corrida
3. Valida se uma transação de débito não excede o limite do cliente
4. Registra a transação e atualiza o saldo do cliente
5. Retorna o novo saldo e limite do cliente

### 3. Endpoints da API

#### Criar Transação
```
POST /clientes/{id}/transacoes
```

A função `criarTransacao()` processa novas transações:
1. Valida o ID do cliente
2. Lê e valida o corpo da requisição
3. Valida regras de negócio (valor positivo, tipo válido, descrição dentro do limite)
4. Efetua a transação e retorna o novo saldo

#### Consultar Extrato
```
GET /clientes/{id}/extrato
```

A função `criarExtrato()` gera o extrato do cliente:
1. Valida o ID do cliente
2. Busca informações de saldo e limite do cliente
3. Busca as últimas 10 transações (limite configurável)
4. Retorna um objeto JSON com saldo atual e transações recentes

### 4. Configuração do Servidor HTTP

A função `main()` configura e inicia o servidor HTTP:
1. Estabelece conexão com o banco de dados
2. Configura as rotas usando o pacote gorilla/mux
3. Define middleware para logging
4. Inicia o servidor HTTP com timeouts configurados

## Fluxo de Dados

1. Cliente faz uma requisição HTTP para um dos endpoints
2. O middleware de logging registra a requisição
3. O handler correspondente valida a requisição
4. O sistema interage com o banco de dados para processar a operação
5. A resposta é formatada em JSON e retornada ao cliente

## Validações e Tratamento de Erros

O sistema implementa validações completas:
- IDs de cliente inválidos
- Valores de transação inválidos (não positivos)
- Tipos de transação inválidos (não 'c' ou 'd')
- Descrições inválidas (vazias ou muito longas)
- Limite de saldo excedido em débitos
- Erros de banco de dados

## Observações Técnicas

1. **Prevenção de Condições de Corrida**: Utiliza bloqueios de linha (FOR UPDATE) para garantir consistência em transações concorrentes.

2. **Pool de Conexões**: Configura limites para conexões simultâneas ao banco para evitar sobrecarga.

3. **Timeouts**: Implementa timeouts para operações de banco de dados e solicitações HTTP.

4. **Logging**: Utiliza middleware para registrar detalhes de cada requisição, incluindo tempo de processamento.

5. **Tratamento de Erros**: Diferencia entre erros de cliente (retornando códigos 4xx apropriados) e erros de servidor (500).

6. **Segurança**: Limita o tamanho das requisições para prevenir ataques de DOS.

## Seção Comentada (Não Ativa)

O código contém uma função comentada `criarTabelas()` que parece ser usada para inicialização do esquema do banco de dados. Esta função:

1. Cria tabelas para clientes e transações
2. Insere dados iniciais para 5 clientes com diferentes limites
3. Adiciona índices para melhorar o desempenho das consultas

Esta função está comentada no código, sugerindo que a inicialização do banco é feita por outro processo (possivelmente scripts de migração externos ou ferramentas de ORM).

## Aspectos de Escalabilidade

1. **Otimização de Banco de Dados**: Uso de índices e limites nas consultas 
2. **Controle de Conexões**: Configuração de pool de conexões
3. **Tratamento de Concorrência**: Uso de bloqueios adequados para evitar condições de corrida