package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
	ddtracer "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
	ddmux "gopkg.in/DataDog/dd-trace-go.v1/contrib/gorilla/mux"
)

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

// Constantes para evitar números mágicos e melhorar legibilidade
var serverPort = os.Getenv("API_PORT")
var agentHost = os.Getenv("DD_AGENT_HOST")
var agentPort = os.Getenv("DD_TRACE_AGENT_PORT")
var serviceName = os.Getenv("DD_SERVICE")

const (
	maxDBIdleConns    = 5
	maxDBOpenConns    = 10
	dbConnMaxLifetime = 5 * time.Minute
	minClientID       = 1
	maxClientID       = 5
	maxDescricaoLen   = 10
	maxTransactions   = 10
)

// Erros personalizados para melhor tratamento
var (
	ErrLimiteExcedido    = errors.New("limite excedido")
	ErrClienteInvalido   = errors.New("cliente inválido")
	ErrValorInvalido     = errors.New("valor inválido")
	ErrTipoInvalido      = errors.New("tipo inválido")
	ErrDescricaoInvalida = errors.New("descrição inválida")
)

func conectarBanco() (*sql.DB, error) {
	var db *sql.DB
	var err error

	for i := 0; i < 10; i++ {
		db, err = sql.Open("mysql", "root:root@tcp(db:3306)/api?parseTime=true")
		if err != nil {
			log.Printf("Tentativa %d: erro ao abrir conexão com banco: %s", i+1, err)
			time.Sleep(2 * time.Second)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = db.PingContext(ctx)
		if err != nil {
			log.Printf("Tentativa %d: banco ainda indisponível: %s", i+1, err)
			time.Sleep(2 * time.Second)
			continue
		}

		// Conexão OK
		db.SetMaxIdleConns(maxDBIdleConns)
		db.SetMaxOpenConns(maxDBOpenConns)
		db.SetConnMaxLifetime(dbConnMaxLifetime)

		log.Println("Conexão com banco de dados estabelecida com sucesso.")
		return db, nil
	}

	return nil, fmt.Errorf("não foi possível conectar ao banco de dados após várias tentativas: %w", err)
}

/*
	func criarTabelas(db *sql.DB) error {
		// Usando transações para garantir atomicidade
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("erro ao iniciar transação: %w", err)
		}
		defer func() {
			if err != nil {
				tx.Rollback()
			}
		}()

		// Excluir tabelas
		_, err = tx.Exec("DROP TABLE IF EXISTS transacoes")
		if err != nil {
		return fmt.Errorf("erro ao excluir tabela transacoes: %w", err)
		}

		_, err = tx.Exec("DROP TABLE IF EXISTS clientes")
		if err != nil {
			return fmt.Errorf("erro ao excluir tabela clientes: %w", err)
		}

		// Criar tabela clientes
		_, err = tx.Exec(`
		CREATE TABLE clientes IF NOT EXISTS(
			id int PRIMARY KEY,
			limite int NOT NULL,
			saldo int NOT NULL
		)`)
		if err != nil {
			return fmt.Errorf("erro ao criar tabela clientes: %w", err)
		}

		// Inserir dados iniciais conforme especificação
		clientesIniciais := []struct {
			id     int
			limite int
			saldo  int
		}{
			{1, 100000, 0},
			{2, 80000, 0},
			{3, 1000000, 0},
			{4, 10000000, 0},
			{5, 500000, 0},
		}

		stmt, err := tx.Prepare("INSERT INTO clientes (id, limite, saldo) VALUES (?, ?, ?)")
		if err != nil {
			return fmt.Errorf("erro ao preparar statement: %w", err)
		}
		defer stmt.Close()

		for _, cliente := range clientesIniciais {
			_, err = stmt.Exec(cliente.id, cliente.limite, cliente.saldo)
			if err != nil {
				return fmt.Errorf("erro ao inserir cliente: %w", err)
			}
		}

		// Criar tabela transacoes
		_, err = tx.Exec(`
		CREATE TABLE transacoes IF NOT EXISTS (
			id int auto_increment primary key,
			valor int NOT NULL,
			tipo char(1) NOT NULL,
			descricao varchar(10) NOT NULL,
			realizada_em TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			cliente_id int NOT NULL,
			FOREIGN KEY (cliente_id) REFERENCES clientes(id)
		)`)
		if err != nil {
			return fmt.Errorf("erro ao criar tabela transacoes: %w", err)
		}

		// Adicionar índice para melhorar a performance de consultas por cliente_id
		_, err = tx.Exec("CREATE INDEX idx_cliente_id ON transacoes(cliente_id)")
		if err != nil {
			return fmt.Errorf("erro ao criar índice: %w", err)
		}

		return tx.Commit()
	}
*/
func salvarTransacao(ctx context.Context, db *sql.DB, transacao TransactionRequest, clienteID uint64) (TransactionSuccessResponse, error) {
	// Usar transação para garantir consistência
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return TransactionSuccessResponse{}, fmt.Errorf("erro ao iniciar transação: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Obter dados do cliente com lock para evitar condições de corrida
	var saldoAtual, limite int64
	err = tx.QueryRowContext(ctx,
		"SELECT saldo, limite FROM clientes WHERE id = ? FOR UPDATE",
		clienteID).Scan(&saldoAtual, &limite)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TransactionSuccessResponse{}, ErrClienteInvalido
		}
		return TransactionSuccessResponse{}, fmt.Errorf("erro ao consultar cliente: %w", err)
	}

	// Processar a transação
	novoSaldo := saldoAtual
	if transacao.Tipo == "d" {
		// Débito (diminui saldo)
		novoSaldo = saldoAtual - transacao.Valor
		if novoSaldo < -limite {
			return TransactionSuccessResponse{}, ErrLimiteExcedido
		}
	} else if transacao.Tipo == "c" {
		// Crédito (aumenta saldo)
		novoSaldo = saldoAtual + transacao.Valor
	} else {
		return TransactionSuccessResponse{}, ErrTipoInvalido
	}

	// Inserir a transação
	_, err = tx.ExecContext(ctx,
		"INSERT INTO transacoes (valor, tipo, descricao, cliente_id) VALUES (?, ?, ?, ?)",
		transacao.Valor, transacao.Tipo, transacao.Descricao, clienteID)
	if err != nil {
		return TransactionSuccessResponse{}, fmt.Errorf("erro ao inserir transação: %w", err)
	}

	// Atualizar o saldo do cliente
	_, err = tx.ExecContext(ctx,
		"UPDATE clientes SET saldo = ? WHERE id = ?",
		novoSaldo, clienteID)
	if err != nil {
		return TransactionSuccessResponse{}, fmt.Errorf("erro ao atualizar saldo: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return TransactionSuccessResponse{}, fmt.Errorf("erro ao finalizar transação: %w", err)
	}

	return TransactionSuccessResponse{
		Limite: limite,
		Saldo:  novoSaldo,
	}, nil
}

func criarExtrato(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Validar ID do cliente
	parametros := mux.Vars(r)
	clienteID, err := strconv.ParseUint(parametros["id"], 10, 64)
	if err != nil {
		http.Error(w, ErrClienteInvalido.Error(), http.StatusNotFound)
		return
	}

	ctx := r.Context()
	db, err := conectarBanco()
	if err != nil {
		log.Printf("Erro na conexão: %v", err)
		http.Error(w, "Erro ao conectar ao banco de dados", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// Obter dados do cliente
	saldo := Saldo{DataExtrato: time.Now()}
	err = db.QueryRowContext(ctx,
		"SELECT saldo, limite FROM clientes WHERE id = ?",
		clienteID).Scan(&saldo.Total, &saldo.Limite)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, ErrClienteInvalido.Error(), http.StatusNotFound)
			return
		}
		log.Printf("Erro ao consultar cliente: %v", err)
		http.Error(w, "Erro ao consultar dados do cliente", http.StatusInternalServerError)
		return
	}

	// Obter transações limitadas às 10 mais recentes
	rows, err := db.QueryContext(ctx,
		"SELECT valor, tipo, descricao, realizada_em FROM transacoes WHERE cliente_id = ? ORDER BY realizada_em DESC LIMIT ?",
		clienteID, maxTransactions)
	if err != nil {
		log.Printf("Erro ao consultar transações: %v", err)
		http.Error(w, "Erro ao consultar transações", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var transacoes []Transacoes
	for rows.Next() {
		var t Transacoes
		var realizadaEmStr string
		if err := rows.Scan(&t.Valor, &t.Tipo, &t.Descricao, &realizadaEmStr); err != nil {
			log.Printf("Erro ao ler transação: %v", err)
			http.Error(w, "Erro ao processar transações", http.StatusInternalServerError)
			return
		}

		// Converter string para time.Time
		//t.RealizadaEm, err = time.Parse("2006-01-02T15:04:05.000Z", realizadaEmStr)
		t.RealizadaEm, err = time.Parse(time.RFC3339Nano, realizadaEmStr)

		if err != nil {
			log.Printf("Erro ao converter data: %v", err)
			http.Error(w, "Erro ao converter data da transação", http.StatusInternalServerError)
			return
		}
		transacoes = append(transacoes, t)
	}

	if err = rows.Err(); err != nil {
		log.Printf("Erro na iteração: %v", err)
		http.Error(w, "Erro ao processar transações", http.StatusInternalServerError)
		return
	}

	extrato := Statement{
		Saldo:             saldo,
		UltimasTransacoes: transacoes,
	}

	if err := json.NewEncoder(w).Encode(extrato); err != nil {
		log.Printf("Erro ao codificar resposta: %v", err)
		http.Error(w, "Erro ao gerar resposta", http.StatusInternalServerError)
		return
	}
}

func criarTransacao(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Validar ID do cliente
	parametros := mux.Vars(r)
	clienteID, err := strconv.ParseUint(parametros["id"], 10, 64)
	if err != nil {
		http.Error(w, ErrClienteInvalido.Error(), http.StatusNotFound)
		return
	}

	// Ler e validar corpo da requisição
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024))
	if err != nil {
		http.Error(w, "Erro ao ler requisição", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var transacao TransactionRequest
	if err := json.Unmarshal(body, &transacao); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}

	// Validações de negócio
	if transacao.Valor <= 0 {
		http.Error(w, ErrValorInvalido.Error(), http.StatusUnprocessableEntity)
		return
	}

	if transacao.Tipo != "d" && transacao.Tipo != "c" {
		http.Error(w, ErrTipoInvalido.Error(), http.StatusUnprocessableEntity)
		return
	}

	if transacao.Descricao == "" || len(transacao.Descricao) > maxDescricaoLen {
		http.Error(w, ErrDescricaoInvalida.Error(), http.StatusUnprocessableEntity)
		return
	}

	ctx := r.Context()
	db, err := conectarBanco()
	if err != nil {
		log.Printf("Erro na conexão: %v", err)
		http.Error(w, "Erro ao conectar ao banco de dados", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// Processar a transação
	resultado, err := salvarTransacao(ctx, db, transacao, clienteID)
	if err != nil {
		switch {
		case errors.Is(err, ErrLimiteExcedido):
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		case errors.Is(err, ErrClienteInvalido):
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		default:
			log.Printf("Erro ao processar transação: %v", err)
			http.Error(w, "Erro ao processar transação", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resultado); err != nil {
		log.Printf("Erro ao codificar resposta: %v", err)
	}
}

func main() {
	
	// Inicia o tracer do Datadog
	ddtracer.Start(
		ddtracer.WithAgentAddr(agentHost + ":" + agentPort),
		ddtracer.WithServiceName(serviceName),
	)
	defer ddtracer.Stop()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Configuração do banco de dados
	db, err := conectarBanco()
	if err != nil {
		log.Fatalf("Erro ao conectar ao banco de dados: %v", err)
	}

	//if err := criarTabelas(db); err != nil {
	//	log.Fatalf("Erro ao configurar tabelas: %v", err)
	//}

	db.Close()
	// Configuração do router
	r := ddmux.NewRouter()
	r.HandleFunc("/clientes/{id}/transacoes", criarTransacao).Methods(http.MethodPost)
	r.HandleFunc("/clientes/{id}/extrato", criarExtrato).Methods(http.MethodGet)

	// Adicionar middleware para logging
	r.Use(loggingMiddleware)

	// Configurar servidor
	srv := &http.Server{
		Addr:         ":" + serverPort,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Iniciar servidor
	log.Printf("Servidor rodando na porta %s", serverPort)
	log.Fatal(srv.ListenAndServe())
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		next.ServeHTTP(w, r)
		log.Printf(
			"%s %s %s %s",
			r.Method,
			r.RequestURI,
			r.RemoteAddr,
			time.Since(startTime),
		)
	})
}
