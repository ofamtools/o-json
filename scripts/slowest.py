# slowest.py DIR [N]: reads go test -json files (DIR/chunk.a?.json, or
# DIR itself when it is one file) and prints each package run's result,
# the failures, the total and the N slowest top-level tests and subtests.
import json, sys, glob, os
src = sys.argv[1]
files = [src] if os.path.isfile(src) else sorted(glob.glob(os.path.join(src, 'chunk.a?.json')))
rows, fails = [], []
for f in files:
    for line in open(f):
        try: e = json.loads(line)
        except ValueError: continue
        if e.get('Test') and e.get('Action') in ('pass', 'fail', 'skip'):
            rows.append((e.get('Elapsed', 0), e['Test'], e['Action']))
            if e['Action'] == 'fail': fails.append(e['Test'])
        if not e.get('Test') and e.get('Action') in ('pass', 'fail'):
            print(f, e['Action'], e.get('Elapsed'))
print('fails:', fails)
top = [r for r in rows if '/' not in r[1]]
print('total top-level seconds: %.1f over %d tests' % (sum(r[0] for r in top), len(top)))
n = int(sys.argv[2]) if len(sys.argv) > 2 else 10
for r in sorted(top, reverse=True)[:n]:
    print('%7.2f %s %s' % r)
print('-- slowest subtests')
for r in sorted([r for r in rows if '/' in r[1]], reverse=True)[:8]:
    print('%7.2f %s %s' % r)
