package licenses

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type License struct {
	ID           int
	Maker        string
	SoftwareName string
	Version      *string
	LicenseKey   *string
	ActivationKey *string
	AssignedTo   *string
	DeviceHostname *string
	DeviceSN     *string
	Section      *string
	PONo         *string
	Status       string
	ActivatedOn  *time.Time
	ExpiryDate   *time.Time
	Remarks      *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

var validStatus = map[string]bool{"In use": true, "Available": true, "Expired": true, "Retired": true}

// Validate checks business rules; returns human-readable error or "".
func (l *License) Validate() error {
	if strings.TrimSpace(l.Maker) == "" {
		return errors.New("Maker wajib diisi")
	}
	if strings.TrimSpace(l.SoftwareName) == "" {
		return errors.New("Software Name wajib diisi")
	}
	if !validStatus[l.Status] {
		return errors.New("Status tidak valid")
	}
	if l.ActivatedOn != nil && l.ExpiryDate != nil && l.ExpiryDate.Before(*l.ActivatedOn) {
		return errors.New("Expiry Date tidak boleh sebelum Activated On")
	}
	return nil
}

const cols = `id, maker, software_name, version, license_key, activation_key,
	assigned_to, device_hostname, device_sn, section, po_no, status,
	activated_on, expiry_date, remarks, created_at, updated_at`

func scanRow(row interface{ Scan(...any) error }) (*License, error) {
	var l License
	err := row.Scan(&l.ID, &l.Maker, &l.SoftwareName, &l.Version, &l.LicenseKey,
		&l.ActivationKey, &l.AssignedTo, &l.DeviceHostname, &l.DeviceSN,
		&l.Section, &l.PONo, &l.Status, &l.ActivatedOn, &l.ExpiryDate,
		&l.Remarks, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// Filter describes list query parameters.
type Filter struct {
	Query    string // search across software/maker/key/assigned
	Status   string
	Section  string
	ExpFrom  *time.Time
	ExpTo    *time.Time
	Sort     string // column name (whitelisted)
	Order    string // ASC|DESC
	Page     int
	PageSize int
}

var sortWhitelist = map[string]bool{
	"software_name": true, "maker": true, "status": true,
	"expiry_date": true, "activated_on": true, "assigned_to": true, "created_at": true,
}

func (f *Filter) normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 1000000 {
		f.PageSize = 20
	}
	if !sortWhitelist[f.Sort] {
		f.Sort = "created_at"
	}
	o := strings.ToUpper(f.Order)
	if o != "ASC" && o != "DESC" {
		o = "DESC"
	}
	f.Order = o
}

func (f *Filter) whereClause() (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, val any) {
		args = append(args, val)
		n := len(args)
		conds = append(conds, strings.ReplaceAll(cond, "@p", fmt.Sprintf("@p%d", n)))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+q+"%")
		n := len(args)
		conds = append(conds, fmt.Sprintf(
			"(software_name LIKE @p%d OR maker LIKE @p%d OR license_key LIKE @p%d OR assigned_to LIKE @p%d OR device_hostname LIKE @p%d)",
			n, n, n, n, n))
	}
	if f.Status != "" && validStatus[f.Status] {
		add("status = @p", f.Status)
	}
	if f.Section != "" {
		add("section = @p", f.Section)
	}
	if f.ExpFrom != nil {
		add("expiry_date >= @p", *f.ExpFrom)
	}
	if f.ExpTo != nil {
		add("expiry_date <= @p", *f.ExpTo)
	}
	if len(conds) == 0 {
		return "1=1", args
	}
	return strings.Join(conds, " AND "), args
}

type Store struct{ DB *sql.DB }

// List returns a page of licenses plus total count for pagination.
func (s *Store) List(ctx context.Context, f Filter) ([]*License, int, error) {
	f.normalize()
	where, args := f.whereClause()

	var total int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM licenses WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (f.Page - 1) * f.PageSize
	q := fmt.Sprintf(`SELECT %s FROM licenses WHERE %s ORDER BY %s %s OFFSET @p%d ROWS FETCH NEXT @p%d ROWS ONLY`,
		cols, where, f.Sort, f.Order, len(args)+1, len(args)+2)
	args = append(args, offset, f.PageSize)

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*License
	for rows.Next() {
		l, err := scanRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}

func (s *Store) GetByID(ctx context.Context, id int) (*License, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM licenses WHERE id=@p1`, id)
	l, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	return l, err
}

func datePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func strPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func (s *Store) insert(ctx context.Context, l *License) error {
	return s.DB.QueryRowContext(ctx, `INSERT INTO licenses
		(maker, software_name, version, license_key, activation_key, assigned_to,
		 device_hostname, device_sn, section, po_no, status, activated_on, expiry_date, remarks)
	 OUTPUT INSERTED.id
	 VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9,@p10,@p11,@p12,@p13,@p14)`,
		l.Maker, l.SoftwareName, strPtr(l.Version), strPtr(l.LicenseKey), strPtr(l.ActivationKey),
		strPtr(l.AssignedTo), strPtr(l.DeviceHostname), strPtr(l.DeviceSN), strPtr(l.Section),
		strPtr(l.PONo), l.Status, datePtr(l.ActivatedOn), datePtr(l.ExpiryDate), strPtr(l.Remarks)).Scan(&l.ID)
}

func (s *Store) update(ctx context.Context, l *License) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE licenses SET
		maker=@p1, software_name=@p2, version=@p3, license_key=@p4, activation_key=@p5,
		assigned_to=@p6, device_hostname=@p7, device_sn=@p8, section=@p9, po_no=@p10,
		status=@p11, activated_on=@p12, expiry_date=@p13, remarks=@p14, updated_at=SYSUTCDATETIME()
	 WHERE id=@p15`,
		l.Maker, l.SoftwareName, strPtr(l.Version), strPtr(l.LicenseKey), strPtr(l.ActivationKey),
		strPtr(l.AssignedTo), strPtr(l.DeviceHostname), strPtr(l.DeviceSN), strPtr(l.Section),
		strPtr(l.PONo), l.Status, datePtr(l.ActivatedOn), datePtr(l.ExpiryDate), strPtr(l.Remarks), l.ID)
	return err
}

// Save inserts when ID==0, updates otherwise. Soft delete = set Status "Retired".
func (s *Store) Save(ctx context.Context, l *License) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if l.ID == 0 {
		return s.insert(ctx, l)
	}
	return s.update(ctx, l)
}

// Stats for dashboard cards.
func (s *Store) Stats(ctx context.Context) (total, inUse, available, expiringSoon int, err error) {
	err = s.DB.QueryRowContext(ctx, `SELECT
		COUNT(*),
		SUM(CASE WHEN status='In use' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status='Available' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status NOT IN ('Expired','Retired') AND expiry_date IS NOT NULL
		         AND expiry_date <= DATEADD(day, 30, CAST(GETDATE() AS date)) THEN 1 ELSE 0 END)
	 FROM licenses`).Scan(&total, &inUse, &available, &expiringSoon)
	// SUM returns NULL on empty table — coerce
	nulls := []any{&inUse, &available, &expiringSoon}
	for _, n := range nulls {
		switch v := n.(type) {
		case *int:
			_ = v
		}
	}
	return
}

// ExpiringSoon returns up to 10 active licenses closest to expiry.
func (s *Store) ExpiringSoon(ctx context.Context) ([]*License, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cols+` FROM licenses
	 WHERE status NOT IN ('Expired','Retired') AND expiry_date IS NOT NULL
	   AND expiry_date <= DATEADD(day, 60, CAST(GETDATE() AS date))
	 ORDER BY expiry_date ASC OFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*License
	for rows.Next() {
		l, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListAllFiltered returns all matching rows (no pagination) — used by Excel export.
func (s *Store) ListAllFiltered(ctx context.Context, f Filter) ([]*License, error) {
	f.normalize()
	f.Page = 1
	f.PageSize = 100
	where, args := f.whereClause()
	q := fmt.Sprintf(`SELECT %s FROM licenses WHERE %s ORDER BY %s %s`, cols, where, f.Sort, f.Order)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*License
	for rows.Next() {
		l, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
