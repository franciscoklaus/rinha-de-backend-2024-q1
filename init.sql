USE api;
DROP TABLE IF EXISTS transacoes;
DROP TABLE IF EXISTS clientes;

CREATE TABLE IF NOT EXISTS clientes (
    id INT PRIMARY KEY,
    limite INT NOT NULL,
    saldo INT NOT NULL
);

INSERT INTO clientes (id, limite, saldo) VALUES (1, 100000, 0);
INSERT INTO clientes (id, limite, saldo) VALUES (2, 80000, 0);
INSERT INTO clientes (id, limite, saldo) VALUES (3, 1000000, 0);
INSERT INTO clientes (id, limite, saldo) VALUES (4, 10000000, 0);
INSERT INTO clientes (id, limite, saldo) VALUES (5, 500000, 0);

CREATE TABLE IF NOT EXISTS transacoes (
    id INT AUTO_INCREMENT PRIMARY KEY,
    valor INT NOT NULL,
    tipo CHAR(1) NOT NULL,
    descricao VARCHAR(10) NOT NULL,
    realizada_em TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    cliente_id INT NOT NULL,
    FOREIGN KEY (cliente_id) REFERENCES clientes(id)
);

CREATE INDEX idx_cliente_id ON transacoes(cliente_id);
