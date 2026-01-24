package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
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
	HydraV1Volts     sql.NullFloat64
	HydraV1Amps      sql.NullFloat64
	HydraV2Volts     sql.NullFloat64
	HydraV2Amps      sql.NullFloat64
	HydraV3Volts     sql.NullFloat64
	HydraV3Amps      sql.NullFloat64
	HydraVinVolts    sql.NullFloat64
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
	statusDrops    int
	runtimeDrops   int
	statusDropLog  time.Time
	runtimeDropLog time.Time
}

type QueryResult struct {
	Columns []string        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
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
		statusCh:  make(chan StatusRow, 20000),
		runtimeCh: make(chan RuntimeRow, 200000),
		closeCh:   make(chan struct{}),
	}
	if err := w.initSchema(); err != nil {
		return nil, err
	}
	w.wg.Add(2)
	go w.runStatus()
	go w.runRuntime()
	return w, nil
}

func (w *Writer) Query(ctx context.Context, sqlQuery string) (*QueryResult, error) {
	conn, err := w.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			log.Println("failed to close query connection:", cerr)
		}
	}()
	if err := w.ensureQueryViews(ctx, conn); err != nil {
		return nil, err
	}
	query := rewriteQueryForHistory(sqlQuery)
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Println("failed to close query rows:", cerr)
		}
	}()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := &QueryResult{
		Columns: cols,
		Rows:    make([][]interface{}, 0),
	}
	for rows.Next() {
		values := make([]interface{}, len(cols))
		scanTargets := make([]interface{}, len(cols))
		for i := range values {
			scanTargets[i] = &values[i]
		}
		if err := rows.Scan(scanTargets...); err != nil {
			return nil, err
		}
		for i, v := range values {
			values[i] = normalizeValue(v)
		}
		result.Rows = append(result.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
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
		w.statusDrops++
		if time.Since(w.statusDropLog) > 10*time.Second {
			log.Printf("status buffer full, dropping rows (dropped=%d)\n", w.statusDrops)
			w.statusDrops = 0
			w.statusDropLog = time.Now()
		}
	}
}

func (w *Writer) EnqueueRuntime(row RuntimeRow) {
	select {
	case w.runtimeCh <- row:
	default:
		w.runtimeDrops++
		if time.Since(w.runtimeDropLog) > 10*time.Second {
			log.Printf("runtime buffer full, dropping rows (dropped=%d)\n", w.runtimeDrops)
			w.runtimeDrops = 0
			w.runtimeDropLog = time.Now()
		}
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
			charger_soc DOUBLE,
			hydra_v1_volts DOUBLE,
			hydra_v1_amps DOUBLE,
			hydra_v2_volts DOUBLE,
			hydra_v2_amps DOUBLE,
			hydra_v3_volts DOUBLE,
			hydra_v3_amps DOUBLE,
			hydra_vin_volts DOUBLE
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
	alterStmts := []string{
		`ALTER TABLE status_hourly ADD COLUMN IF NOT EXISTS hydra_v1_volts DOUBLE`,
		`ALTER TABLE status_hourly ADD COLUMN IF NOT EXISTS hydra_v1_amps DOUBLE`,
		`ALTER TABLE status_hourly ADD COLUMN IF NOT EXISTS hydra_v2_volts DOUBLE`,
		`ALTER TABLE status_hourly ADD COLUMN IF NOT EXISTS hydra_v2_amps DOUBLE`,
		`ALTER TABLE status_hourly ADD COLUMN IF NOT EXISTS hydra_v3_volts DOUBLE`,
		`ALTER TABLE status_hourly ADD COLUMN IF NOT EXISTS hydra_v3_amps DOUBLE`,
		`ALTER TABLE status_hourly ADD COLUMN IF NOT EXISTS hydra_vin_volts DOUBLE`,
	}
	for _, stmt := range alterStmts {
		if _, err := w.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) runStatus() {
	defer w.wg.Done()
	flushTicker := time.NewTicker(2 * time.Second)
	defer flushTicker.Stop()

	statusBatch := make([]StatusRow, 0, 200)

	flushStatus := func() {
		if len(statusBatch) == 0 {
			return
		}
		if err := w.insertStatusBatch(statusBatch); err != nil {
			log.Println("failed to insert status batch:", err)
		}
		statusBatch = statusBatch[:0]
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
				go w.flushStatusHour(w.statusHourUTC)
				w.statusHourUTC = rowHour
			}
			statusBatch = append(statusBatch, row)
			if len(statusBatch) >= 200 {
				flushStatus()
			}

		case <-flushTicker.C:
			flushStatus()

		case <-w.closeCh:
			flushStatus()
			return
		}
	}
}

func (w *Writer) runRuntime() {
	defer w.wg.Done()
	flushTicker := time.NewTicker(100 * time.Millisecond)
	defer flushTicker.Stop()

	runtimeBatch := make([]RuntimeRow, 0, 20000)

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
				go w.flushRuntimeHour(w.runtimeHourUTC)
				w.runtimeHourUTC = rowHour
			}
			runtimeBatch = append(runtimeBatch, row)
			if len(runtimeBatch) >= 20000 {
				flushRuntime()
			}

		case <-flushTicker.C:
			flushRuntime()

		case <-w.closeCh:
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
		gps_lat, gps_lon, charger_state, charger_soc,
		hydra_v1_volts, hydra_v1_amps, hydra_v2_volts, hydra_v2_amps,
		hydra_v3_volts, hydra_v3_amps, hydra_vin_volts
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
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
			row.HydraV1Volts,
			row.HydraV1Amps,
			row.HydraV2Volts,
			row.HydraV2Amps,
			row.HydraV3Volts,
			row.HydraV3Amps,
			row.HydraVinVolts,
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
	dir := filepath.Join(
		w.baseDir,
		"status",
		fmt.Sprintf("year=%04d", start.Year()),
		fmt.Sprintf("month=%02d", start.Month()),
		fmt.Sprintf("day=%02d", start.Day()),
		fmt.Sprintf("hour=%02d", start.Hour()),
	)
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
	dir := filepath.Join(
		w.baseDir,
		"runtime",
		fmt.Sprintf("year=%04d", start.Year()),
		fmt.Sprintf("month=%02d", start.Month()),
		fmt.Sprintf("day=%02d", start.Day()),
		fmt.Sprintf("hour=%02d", start.Hour()),
	)
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

func normalizeValue(val interface{}) interface{} {
	switch v := val.(type) {
	case nil:
		return nil
	case []byte:
		return string(v)
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		return v
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return f
	default:
		return v
	}
}

func (w *Writer) ensureQueryViews(ctx context.Context, conn *sql.Conn) error {
	runtimeParquet := filepath.Join(w.baseDir, "runtime")
	statusParquet := filepath.Join(w.baseDir, "status")
	hasRuntimeParquet := hasParquet(runtimeParquet)
	hasStatusParquet := hasParquet(statusParquet)

	if err := w.createHistoryView(ctx, conn, "runtime_metrics_all", "runtime_metrics", runtimeParquet, hasRuntimeParquet, "runtime.parquet"); err != nil {
		return err
	}
	if err := w.createHistoryView(ctx, conn, "status_hourly_all", "status_hourly", statusParquet, hasStatusParquet, "status.parquet"); err != nil {
		return err
	}
	return nil
}

func (w *Writer) createHistoryView(ctx context.Context, conn *sql.Conn, viewName string, tableName string, baseDir string, hasParquet bool, fileName string) error {
	if !hasParquet {
		_, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE OR REPLACE TEMP VIEW %s AS SELECT * FROM %s", viewName, tableName))
		return err
	}
	hiveGlob := filepath.ToSlash(filepath.Join(baseDir, "year=*", "month=*", "day=*", "hour=*", fileName))
	stmt := fmt.Sprintf(
		`CREATE OR REPLACE TEMP VIEW %s AS
SELECT * FROM %s
UNION ALL SELECT * FROM read_parquet('%s', hive_partitioning=1)`,
		viewName,
		tableName,
		escapePath(hiveGlob),
	)
	_, err := conn.ExecContext(ctx, stmt)
	return err
}

func hasParquet(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".parquet") {
			found = true
			return fs.SkipDir
		}
		return nil
	})
	return found
}

func rewriteQueryForHistory(sqlQuery string) string {
	replacements := map[string]string{
		"runtime_metrics": "runtime_metrics_all",
		"status_hourly":   "status_hourly_all",
	}
	out := sqlQuery
	for src, dst := range replacements {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(src) + `\b`)
		out = re.ReplaceAllString(out, dst)
	}
	return out
}
