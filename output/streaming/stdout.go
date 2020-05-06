package streaming

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/pkg/errors"

	"github.com/cube2222/octosql/execution"
	"github.com/cube2222/octosql/storage"
)

type StreamPrinter struct {
	stateStorage storage.Storage
	rs           execution.RecordStream
	printfn      func(record *execution.Record)
}

func NewStreamPrinter(stateStorage storage.Storage, recordStream execution.RecordStream, printfn func(record *execution.Record)) *StreamPrinter {
	return &StreamPrinter{
		stateStorage: stateStorage,
		rs:           recordStream,
		printfn:      printfn,
	}
}

func (sp *StreamPrinter) Run(ctx context.Context) error {
	for {
		tx := sp.stateStorage.BeginTransaction()
		ctx := storage.InjectStateTransaction(ctx, tx)

		rec, err := sp.rs.Next(ctx)
		if err == execution.ErrEndOfStream {
			err := tx.Commit()
			if err != nil {
				continue
			}
			break
		} else if errors.Cause(err) == execution.ErrNewTransactionRequired {
			err := tx.Commit()
			if err != nil {
				continue
			}
			continue
		} else if waitableError := execution.GetErrWaitForChanges(err); waitableError != nil {
			err := tx.Commit()
			if err != nil {
				continue
			}
			err = waitableError.ListenForChanges(ctx)
			if err != nil {
				log.Println("couldn't listen for changes: ", err)
			}
			err = waitableError.Close()
			if err != nil {
				log.Println("couldn't close subscription: ", err)
			}
			continue
		} else if err != nil {
			return errors.Wrap(err, "couldn't get next record")
		}

		sp.printfn(rec)

		err = tx.Commit()
		if err != nil {
			log.Println("error committing output print transaction (this can lead to duplicate output records): ", err)
			continue
		}
	}
	return nil
}

func JSONPrinter() func(rec *execution.Record) {
	w := bufio.NewWriter(os.Stdout)
	enc := json.NewEncoder(w)

	return func(rec *execution.Record) {
		kvs := make(map[string]interface{})
		for _, field := range rec.ShowFields() {
			kvs[field.Name.String()] = rec.Value(field.Name).ToRawValue()
		}

		if err := enc.Encode(kvs); err != nil {
			log.Println("error encoding record for output print: ", err)
		}
		if err := w.Flush(); err != nil {
			log.Println("error flushing stdout buffered writer: ", err)
		}
	}
}