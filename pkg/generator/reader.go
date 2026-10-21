package generator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"coderpush/pkg/domain"
)

type jsonBatch struct {
	JobID      string       `json:"job_id"`
	BatchIndex int          `json:"batch_index"`
	Records    []jsonRecord `json:"records"`
}

type jsonRecord struct {
	SKU       string               `json:"sku"`
	Name      string               `json:"name"`
	Price     any                  `json:"price"`
	Status    domain.ProductStatus `json:"status"`
	Timestamp time.Time            `json:"timestamp"`
}

func toJSONRecord(r domain.SupplierUpdate) jsonRecord {
	var p any = r.Price
	if math.IsNaN(r.Price) {
		p = "NaN"
	} else if math.IsInf(r.Price, 1) {
		p = "+Infinity"
	} else if math.IsInf(r.Price, -1) {
		p = "-Infinity"
	}
	return jsonRecord{
		SKU:       r.SKU,
		Name:      r.Name,
		Price:     p,
		Status:    r.Status,
		Timestamp: r.Timestamp,
	}
}

func fromJSONRecord(jr jsonRecord) domain.SupplierUpdate {
	var price float64
	switch v := jr.Price.(type) {
	case float64:
		price = v
	case string:
		switch v {
		case "NaN":
			price = math.NaN()
		case "+Infinity", "Infinity", "+Inf", "Inf":
			price = math.Inf(1)
		case "-Infinity", "-Inf":
			price = math.Inf(-1)
		default:
			price = math.NaN()
		}
	case nil:
		price = math.NaN()
	}
	return domain.SupplierUpdate{
		SKU:       jr.SKU,
		Name:      jr.Name,
		Price:     price,
		Status:    jr.Status,
		Timestamp: jr.Timestamp,
	}
}

// MarshalRecordJSON serializes a SupplierUpdate into JSON, supporting IEEE 754 NaN and Inf.
func MarshalRecordJSON(rec domain.SupplierUpdate) ([]byte, error) {
	return json.Marshal(toJSONRecord(rec))
}

// MarshalBatchJSON serializes a Batch into JSON, supporting IEEE 754 NaN and Inf.
func MarshalBatchJSON(batch domain.Batch) ([]byte, error) {
	records := make([]jsonRecord, len(batch.Records))
	for i, r := range batch.Records {
		records[i] = toJSONRecord(r)
	}
	jb := jsonBatch{
		JobID:      batch.JobID,
		BatchIndex: batch.BatchIndex,
		Records:    records,
	}
	return json.Marshal(jb)
}

// UnmarshalBatchJSON deserializes a JSON byte slice into a Batch.
func UnmarshalBatchJSON(data []byte) (domain.Batch, error) {
	var jb jsonBatch
	if err := json.Unmarshal(data, &jb); err != nil {
		return domain.Batch{}, err
	}
	records := make([]domain.SupplierUpdate, len(jb.Records))
	for i, jr := range jb.Records {
		records[i] = fromJSONRecord(jr)
	}
	return domain.Batch{
		JobID:      jb.JobID,
		BatchIndex: jb.BatchIndex,
		Records:    records,
	}, nil
}

// ReadJSONBatches reads newline-delimited batches from an io.Reader and delivers them on a channel.
func ReadJSONBatches(ctx context.Context, r io.Reader) (<-chan domain.Batch, <-chan error) {
	ch := make(chan domain.Batch, 4)
	errCh := make(chan error, 1)

	go func() {
		defer close(ch)
		defer close(errCh)

		scanner := bufio.NewScanner(r)
		// Allow up to 10MB per batch line for large batch payloads
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 10*1024*1024)

		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}

			batch, err := UnmarshalBatchJSON(line)
			if err != nil {
				errCh <- fmt.Errorf("failed to decode batch JSON: %w", err)
				return
			}

			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case ch <- batch:
			}
		}

		if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
			errCh <- fmt.Errorf("scanner error reading batch stream: %w", err)
		}
	}()

	return ch, errCh
}
