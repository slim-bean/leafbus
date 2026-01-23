package store

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

const (
	defaultDBFile = "leafbus.duckdb"
)

type StatusRow struct {
	Timestamp        time.Time
	Battery12VSOC    sql.NullFloat64
	Battery12VVolts  sql.NullFloat64
	Battery12VAmps   sql.NullFloat64
	Battery12VTempC  sql.NullFloat64
	Battery12VTemps  sql.NullString
	Battery12VStatus sql.NullString
	TractionSOC      sql.NullFloat64
	TractionTempC    sql.NullFloat64
	GPSLat           sql.NullFloat64
	GPSLon           sql.NullFloat64
	ChargerState     sql.NullString
	ChargerSOC       sql.NullFloat64
}

type RuntimeRow struct {
	Timestamp time.Time
	Name      string
	Value     sql.NullFloat64
	Text      sql.NullString
	Labels    sql.NullString
	Kind      sql.NullString
}

type Writer struct {
	db             *sql.DB
	baseDir        string
	statusCh       chan StatusRow
	runtimeCh      chan RuntimeRow
	closeCh        chan struct{}
	wg             sync.WaitGroup
	statusHourUTC  time.Time
	runtimeHourUTC time.Time
}

func NewWriter(baseDir string, dbPath string) (*Writer, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("parquet base dir is required")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, err
	}
	if dbPath == "" {
		dbPath = filepath.Join(baseDir, defaultDBFile)
	}
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, err
	}
	w := &Writer{
		db:        db,
		baseDir:   baseDir,
		statusCh:  make(chan StatusRow, 2000),
		runtimeCh: make(chan RuntimeRow, 5000),
		closeCh:   make(chan struct{}),
	}
	if err := w.initSchema(); err != nil {
		return nil, err
	}
	w.wg.Add(1)
	go w.run()
	return w, nil
}

func (w *Writer) Close() {
	close(w.closeCh)
	w.wg.Wait()
	if err := w.db.Close(); err != nil {
		log.Println("failed to close duckdb:", err)
	}
}

func (w *Writer) EnqueueStatus(row StatusRow) {
	select {
	case w.statusCh <- row:
	default:
		log.Println("status buffer full, dropping row")
	}
}

func (w *Writer) EnqueueRuntime(row RuntimeRow) {
	select {
	case w.runtimeCh <- row:
	default:
		log.Println("runtime buffer full, dropping row")
	}
}

func (w *Writer) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS status_hourly (
			ts TIMESTAMP,
			battery12v_soc DOUBLE,
			battery12v_volts DOUBLE,
			battery12v_amps DOUBLE,
			battery12v_temp_c DOUBLE,
			battery12v_temps VARCHAR,
			battery12v_status VARCHAR,
			traction_soc DOUBLE,
			traction_temp_c DOUBLE,
			gps_lat DOUBLE,
			gps_lon DOUBLE,
			charger_state VARCHAR,
			charger_soc DOUBLE
		);`,
		`CREATE TABLE IF NOT EXISTS runtime_metrics (
			ts TIMESTAMP,
			name VARCHAR,
			value DOUBLE,
			text VARCHAR,
			labels VARCHAR,
			kind VARCHAR
		);`,
	}
	for _, stmt := range stmts {
		if _, err := w.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) run() {
	defer w.wg.Done()
	flushTicker := time.NewTicker(2 * time.Second)
	defer flushTicker.Stop()

	statusBatch := make([]StatusRow, 0, 200)
	runtimeBatch := make([]RuntimeRow, 0, 400)

	flushStatus := func() {
		if len(statusBatch) == 0 {
			return
		}
		if err := w.insertStatusBatch(statusBatch); err != nil {
			log.Println("failed to insert status batch:", err)
		}
		statusBatch = statusBatch[:0]
	}

	flushRuntime := func() {
		if len(runtimeBatch) == 0 {
			return
		}
		if err := w.insertRuntimeBatch(runtimeBatch); err != nil {
			log.Println("failed to insert runtime batch:", err)
		}
		runtimeBatch = runtimeBatch[:0]
	}

	for {
		select {
		case row := <-w.statusCh:
			if row.Timestamp.IsZero() {
				row.Timestamp = time.Now().UTC()
			} else {
				row.Timestamp = row.Timestamp.UTC()
			}
			rowHour := row.Timestamp.Truncate(time.Hour)
			if w.statusHourUTC.IsZero() {
				w.statusHourUTC = rowHour
			}
			if !rowHour.Equal(w.statusHourUTC) {
				flushStatus()
				w.flushStatusHour(w.statusHourUTC)
				w.statusHourUTC = rowHour
			}
			statusBatch = append(statusBatch, row)
			if len(statusBatch) >= 200 {
				flushStatus()
			}

		case row := <-w.runtimeCh:
			if row.Timestamp.IsZero() {
				row.Timestamp = time.Now().UTC()
			} else {
				row.Timestamp = row.Timestamp.UTC()
			}
			rowHour := row.Timestamp.Truncate(time.Hour)
			if w.runtimeHourUTC.IsZero() {
				w.runtimeHourUTC = rowHour
			}
			if !rowHour.Equal(w.runtimeHourUTC) {
				flushRuntime()
				w.flushRuntimeHour(w.runtimeHourUTC)
				w.runtimeHourUTC = rowHour
			}
			runtimeBatch = append(runtimeBatch, row)
			if len(runtimeBatch) >= 400 {
				flushRuntime()
			}

		case <-flushTicker.C:
			flushStatus()
			flushRuntime()

		case <-w.closeCh:
			flushStatus()
			flushRuntime()
			return
		}
	}
}

func (w *Writer) insertStatusBatch(rows []StatusRow) error {
	tx, err := w.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO status_hourly (
		ts, battery12v_soc, battery12v_volts, battery12v_amps, battery12v_temp_c,
		battery12v_temps, battery12v_status, traction_soc, traction_temp_c,
		gps_lat, gps_lon, charger_state, charger_soc
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer func() {
		if cerr := stmt.Close(); cerr != nil {
			log.Println("failed to close status stmt:", cerr)
		}
	}()
	for _, row := range rows {
		_, err = stmt.Exec(
			row.Timestamp,
			row.Battery12VSOC,
			row.Battery12VVolts,
			row.Battery12VAmps,
			row.Battery12VTempC,
			row.Battery12VTemps,
			row.Battery12VStatus,
			row.TractionSOC,
			row.TractionTempC,
			row.GPSLat,
			row.GPSLon,
			row.ChargerState,
			row.ChargerSOC,
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (w *Writer) insertRuntimeBatch(rows []RuntimeRow) error {
	tx, err := w.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO runtime_metrics (
		ts, name, value, text, labels, kind
	) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer func() {
		if cerr := stmt.Close(); cerr != nil {
			log.Println("failed to close runtime stmt:", cerr)
		}
	}()
	for _, row := range rows {
		_, err = stmt.Exec(
			row.Timestamp,
			row.Name,
			row.Value,
			row.Text,
			row.Labels,
			row.Kind,
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (w *Writer) flushStatusHour(hour time.Time) {
	if hour.IsZero() {
		return
	}
	start := hour.UTC()
	end := start.Add(time.Hour)
	dir := filepath.Join(w.baseDir, "status", start.Format("2006/01/02/15"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Println("failed to create status parquet dir:", err)
		return
	}
	filePath := filepath.Join(dir, "status.parquet")
	w.copyAndDelete(
		"status_hourly",
		start,
		end,
		filePath,
	)
}

func (w *Writer) flushRuntimeHour(hour time.Time) {
	if hour.IsZero() {
		return
	}
	start := hour.UTC()
	end := start.Add(time.Hour)
	dir := filepath.Join(w.baseDir, "runtime", start.Format("2006/01/02/15"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Println("failed to create runtime parquet dir:", err)
		return
	}
	filePath := filepath.Join(dir, "runtime.parquet")
	w.copyAndDelete(
		"runtime_metrics",
		start,
		end,
		filePath,
	)
}

func (w *Writer) copyAndDelete(table string, start, end time.Time, filePath string) {
	startLiteral := timestampLiteral(start)
	endLiteral := timestampLiteral(end)
	copySQL := fmt.Sprintf(
		"copy (select * from %s where ts >= %s and ts < %s) to '%s' (format parquet, overwrite true)",
		table,
		startLiteral,
		endLiteral,
		escapePath(filePath),
	)
	if _, err := w.db.Exec(copySQL); err != nil {
		log.Println("failed to copy parquet:", err)
		return
	}
	deleteSQL := fmt.Sprintf(
		"delete from %s where ts >= %s and ts < %s",
		table,
		startLiteral,
		endLiteral,
	)
	if _, err := w.db.Exec(deleteSQL); err != nil {
		log.Println("failed to delete after copy:", err)
	}
}

func timestampLiteral(ts time.Time) string {
	return fmt.Sprintf("TIMESTAMP '%s'", ts.UTC().Format("2006-01-02 15:04:05"))
}

func escapePath(path string) string {
	return strings.ReplaceAll(path, "'", "''")
}
