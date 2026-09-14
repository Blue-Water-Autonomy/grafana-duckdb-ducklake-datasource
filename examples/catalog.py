"""Shared connections for the two example DuckLake writers."""
import os

import duckdb


def literal(value):
    return "'" + value.replace("'", "''") + "'"


def connect(tls=False):
    db = duckdb.connect(config={'extension_directory': '/opt/duckdb/extensions'})
    db.execute('LOAD ducklake; LOAD postgres')
    host = 'postgres-tls' if tls else 'postgres'
    db.execute("CREATE SECRET writer (TYPE postgres, HOST " + literal(host)
               + ", DATABASE 'ducklake', USER 'ducklake', PASSWORD "
               + literal(os.environ['DUCKLAKE_PASSWORD']) + ')')
    parameters = 'sslmode=disable'
    if tls:
        parameters = ('sslmode=verify-full sslrootcert=/client-tls/ca.crt '
                      'sslcert=/client-tls/writer.crt sslkey=/client-tls/writer.key')
    db.execute("ATTACH " + literal('ducklake:postgres:' + parameters) + " AS lake ("
               "METADATA_PARAMETERS MAP {'SECRET': 'writer'}, DATA_PATH "
               + literal('/var/lib/ducklake/tls' if tls else '/var/lib/ducklake/local')
               + ', OVERRIDE_DATA_PATH false)')
    return db
