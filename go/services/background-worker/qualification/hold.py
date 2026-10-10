"""Own the synthetic meeting row lock until explicit release or bounded expiry."""
from pathlib import Path
import subprocess
import time
if __name__=='__main__':
    child=subprocess.Popen(['/fixtures/switchback-storage','lock-meeting'],stdout=subprocess.PIPE,text=True)
    try:
        if child.stdout.readline().strip()!='{"lock":"held"}':raise ValueError('fixture lock absent')
        Path('/reports/lock-ready').touch();deadline=time.monotonic()+180
        while not Path('/reports/lock-release').exists():
            if child.poll() is not None:raise ValueError('fixture lock exited')
            if time.monotonic()>deadline:raise ValueError('fixture lock expired')
            time.sleep(.1)
    finally:
        child.terminate();child.wait(timeout=5)
        Path('/reports/lock-stopped').touch()
