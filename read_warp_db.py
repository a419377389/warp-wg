import sqlite3
import json

db_path = r'C:\Users\Administrator\AppData\Local\Warp\Warp\data\warp.sqlite'
conn = sqlite3.connect(db_path)
cursor = conn.cursor()

print("=== 所有 generic_string_objects 记录 ===")
cursor.execute("SELECT id, data FROM generic_string_objects")
for row in cursor.fetchall():
    try:
        data = json.loads(row[1])
        if 'base_model' in data or 'model' in str(data):
            print(f"\nID {row[0]}:")
            print(json.dumps(data, indent=2, ensure_ascii=False))
    except:
        pass

conn.close()
