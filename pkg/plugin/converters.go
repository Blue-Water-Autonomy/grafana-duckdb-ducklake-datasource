package plugin

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"regexp"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana-plugin-sdk-go/data/sqlutil"
)

type rawValue struct{ value any }

func (r *rawValue) Scan(value any) error { r.value = value; return nil }

func nullableConverter[T any](typeName string, fieldType data.FieldType) sqlutil.Converter {
	return sqlutil.Converter{
		InputTypeName: typeName,
		InputScanType: reflect.TypeOf(sql.Null[T]{}),
		FrameConverter: sqlutil.FrameConverter{FieldType: fieldType, ConverterFunc: func(in any) (any, error) {
			value := in.(*sql.Null[T])
			if !value.Valid {
				return (*T)(nil), nil
			}
			return &value.V, nil
		}},
	}
}

func converters() []sqlutil.Converter {
	return []sqlutil.Converter{
		nullableConverter[int8]("TINYINT", data.FieldTypeNullableInt8),
		nullableConverter[int16]("SMALLINT", data.FieldTypeNullableInt16),
		nullableConverter[uint8]("UTINYINT", data.FieldTypeNullableUint8),
		nullableConverter[uint16]("USMALLINT", data.FieldTypeNullableUint16),
		nullableConverter[uint32]("UINTEGER", data.FieldTypeNullableUint32),
		nullableConverter[uint64]("UBIGINT", data.FieldTypeNullableUint64),
		nullableConverter[float32]("FLOAT", data.FieldTypeNullableFloat32),
		{
			InputTypeName: "UUID", InputScanType: reflect.TypeOf(rawValue{}),
			FrameConverter: sqlutil.FrameConverter{FieldType: data.FieldTypeNullableString, ConverterFunc: func(in any) (any, error) {
				value := in.(*rawValue).value
				if value == nil {
					return (*string)(nil), nil
				}
				var uuid duckdb.UUID
				if err := uuid.Scan(value); err != nil {
					return nil, err
				}
				text := uuid.String()
				return &text, nil
			}},
		},
		{
			InputTypeRegex: regexp.MustCompile(`^DECIMAL`), InputScanType: reflect.TypeOf(rawValue{}),
			FrameConverter: sqlutil.FrameConverter{FieldType: data.FieldTypeNullableFloat64, ConverterFunc: func(in any) (any, error) {
				value := in.(*rawValue).value
				if value == nil {
					return (*float64)(nil), nil
				}
				decimal, ok := value.(duckdb.Decimal)
				if !ok {
					return nil, fmt.Errorf("unexpected decimal type %T", value)
				}
				number := decimal.Float64()
				return &number, nil
			}},
		},
		{
			InputTypeRegex: regexp.MustCompile(`^(HUGEINT|UHUGEINT|BIGNUM|INTERVAL|BLOB|BIT|STRUCT.*|MAP.*|UNION.*|.*\[.*\])$`),
			InputScanType:  reflect.TypeOf(rawValue{}),
			FrameConverter: sqlutil.FrameConverter{FieldType: data.FieldTypeNullableString, ConverterFunc: func(in any) (any, error) {
				value := in.(*rawValue).value
				if value == nil {
					return (*string)(nil), nil
				}
				var text string
				switch v := value.(type) {
				case string:
					text = v
				case *big.Int:
					text = v.String()
				case []byte:
					text = base64.StdEncoding.EncodeToString(v)
				default:
					encoded, err := json.Marshal(v)
					if err != nil {
						return nil, err
					}
					text = string(encoded)
				}
				return &text, nil
			}},
		},
	}
}
