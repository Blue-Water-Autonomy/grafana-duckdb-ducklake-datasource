"""Publish fresh measurements to both lakes from a separate process."""
import math
import time

from catalog import connect

connections = [connect(tls=False), connect(tls=True)]
try:
    while True:
        for db in connections:
            for sensor in range(1, 4):
                value = 19 + sensor * 2 + math.sin(time.time() / 90 + sensor) * 3
                db.execute('INSERT INTO lake.measurements VALUES (now(), ?, ?)', [f'sensor-{sensor}', value])
            # Bound the demo's live history while retaining enough data for the dashboard.
            db.execute("DELETE FROM lake.measurements WHERE time < now() - INTERVAL '6 hours'")
        print('Inserted measurements into both DuckLake catalogs.', flush=True)
        time.sleep(5)
finally:
    for db in connections:
        db.close()
