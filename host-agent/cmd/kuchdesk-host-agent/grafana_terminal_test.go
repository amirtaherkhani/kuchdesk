package main

import (
	"os"
	"os/exec"
	"testing"
)

// Use a real controlling PTY and fake kubectl. This catches echo leaks and
// terminal damage that an injected in-memory input reader cannot detect.
func TestGrafanaResetRealTerminalHidesInputAndRestoresEcho(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for controlling-terminal integration test")
	}
	cmd := exec.Command(python, "-c", grafanaPTYTest, os.Args[0])
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("terminal integration failed: %v: %s", err, output)
	}
}

const grafanaPTYTest = `
import json, os, pathlib, pty, select, signal, sys, tempfile, termios, time
with tempfile.TemporaryDirectory(prefix='grafana-pty-dummy-') as td:
    root=pathlib.Path(td)
    fake=root/'kubectl'
    fake.write_text('#!'+sys.executable+'\nimport json,os,sys\nwith open(os.environ["DUMMY_CAPTURE"],"w") as f: json.dump({"args":sys.argv[1:],"stdin":sys.stdin.read()},f)\nprint("Dummy-password-only!")\n')
    fake.chmod(0o700)
    for mode in ('success','cancel','interrupt','eof','redirect'):
        capture=root/'capture'
        if capture.exists(): capture.unlink()
        pid, master=pty.fork()
        if pid==0:
            env=dict(os.environ,PATH=td+':'+os.environ.get('PATH',''),DUMMY_CAPTURE=str(capture),KUCHDESK_TEST_GRAFANA_CLI='1',KUCHDESK_TEST_GRAFANA_ARGS='grafana-admin-password-reset\n--kube-context\ndummy-context\n--user-id\n42')
            if mode=='redirect':
                null=os.open(os.devnull,os.O_WRONLY);os.dup2(null,2);os.close(null)
            os.execve(sys.argv[1],[sys.argv[1],'-test.run=^TestGrafanaCLIProcess$'],env)
        transcript=b''; step=0; completed=False; deadline=time.monotonic()+8
        while time.monotonic()<deadline:
            ready,_,_=select.select([master],[],[],0.03)
            if ready:
                try: data=os.read(master,8192)
                except OSError: completed=True;break
                if not data: completed=True;break
                transcript+=data
            prompt=(b'New Grafana admin password (hidden):',b'Confirm new password (hidden):',b'Type RESET GRAFANA ADMIN PASSWORD')[min(step,2)]
            if step<3 and prompt in transcript:
                if step<2:
                    if termios.tcgetattr(master)[3]&termios.ECHO: continue
                    if mode=='interrupt': os.kill(pid,signal.SIGINT);step=3;continue
                    if mode=='eof': os.write(master,b'\x04');step=3;continue
                    os.write(master,b'Dummy-password-only!\n')
                else:
                    os.write(master,b'RESET GRAFANA ADMIN PASSWORD\n' if mode=='success' else b'cancel\n')
                step+=1
        if not completed: os.kill(pid,signal.SIGKILL)
        _,status=os.waitpid(pid,0)
        echo=bool(termios.tcgetattr(master)[3]&termios.ECHO)
        os.close(master)
        assert completed, 'terminal prompt stalled: '+mode
        assert echo, 'terminal echo not restored: '+mode
        assert b'Dummy-password-only!' not in transcript, 'password or subprocess output leaked: '+mode
        if mode=='success':
            assert os.waitstatus_to_exitcode(status)==0 and capture.exists(), 'reset did not submit: '+transcript.decode(errors='replace').replace('Dummy-password-only!','<dummy>')
            saved=json.loads(capture.read_text())
            assert saved['stdin']=='Dummy-password-only!\n', 'incorrect stdin'
            assert 'Dummy-password-only!' not in ' '.join(saved['args']), 'password in argv'
        else:
            assert os.waitstatus_to_exitcode(status)!=0 and not capture.exists(), 'cancel/redirect submitted reset'
print('real terminal security checks passed')
`
