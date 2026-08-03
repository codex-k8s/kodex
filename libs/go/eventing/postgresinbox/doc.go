// Package postgresinbox реализует provider-neutral durable PostgreSQL inbox,
// который через узкую transaction-bound capability атомарно фиксирует consumer
// effect, inbox evidence и cursor и предоставляет bounded operator recovery.
package postgresinbox
