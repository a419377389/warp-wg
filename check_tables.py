import sqlite3
import json
conn = sqlite3.connect(r'C:\Users\Administrator\AppData\Local\Warp\Warp\data\warp.sqlite')
cursor = conn.cursor()

print("=== generic_string_objects - looking for agent profiles ===")
cursor.execute("SELECT id, data FROM generic_string_objects")
for row in cursor.fetchall():
    try:
        data = json.loads(row[1])
        # Look for agent profile related data
        data_str = str(data).lower()
        if 'profile' in data_str or 'agent' in data_str or 'default' in data_str or 'claude' in data_str or 'base_model' in data_str:
            print(f"\nID: {row[0]}")
            print(json.dumps(data, indent=2, ensure_ascii=False)[:500])
    except:
        pass

conn.close()
