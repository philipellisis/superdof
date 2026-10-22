using System;
using System.Collections.Concurrent;
using System.ComponentModel.Composition;
using System.IO;
using System.IO.Pipes;
using System.Text;
using System.Threading;
using B2SServerPluginInterface;

namespace SuperDOF.B2SBridge
{
    /// <summary>
    /// Drop-in replacement for the DirectOutput B2S server plugin
    /// (B2SServerDirectOutputPlugin.dll). Instead of hosting the DOF framework
    /// in the B2S server process it forwards table element updates over the
    /// named pipe "SuperDOF" to the SuperDOF service (superdof.exe), which
    /// drives the PinOne board.
    /// </summary>
    [Export(typeof(IDirectPlugin))]
    public class Plugin : IDirectPlugin, IDirectPluginPinMame
    {
        private const string PipeName = "SuperDOF";
        private readonly BlockingCollection<string> _queue = new BlockingCollection<string>(8192);
        private Thread _worker;
        private volatile bool _run;

        public string Name { get { return "SuperDOF bridge (PinOne) 0.1"; } }

        public void PluginInit(string tableFilename, string romName)
        {
            _run = true;
            _worker = new Thread(Pump) { IsBackground = true, Name = "SuperDOF pipe" };
            _worker.Start();
            _queue.Add("INIT|" + Clean(tableFilename) + "|" + Clean(romName));
        }

        public void PluginFinish()
        {
            try { _queue.Add("FINISH"); } catch { }
            _run = false;
            try { _queue.CompleteAdding(); } catch { }
            if (_worker != null) _worker.Join(1500);
        }

        public void DataReceive(char tableElementTypeChar, int number, int value)
        {
            if (_queue.IsAddingCompleted) return;
            // drop instead of blocking the emulator when the service is gone
            _queue.TryAdd("D|" + tableElementTypeChar + "|" + number + "|" + value);
        }

        public void PinMameRun() { }
        public void PinMamePause() { }
        public void PinMameContinue() { }
        public void PinMameStop() { }

        private static string Clean(string s)
        {
            return (s ?? "").Replace("|", "_").Replace("\r", "").Replace("\n", "");
        }

        private void Pump()
        {
            NamedPipeClientStream pipe = null;
            StreamWriter w = null;
            foreach (var line in _queue.GetConsumingEnumerable())
            {
                for (int attempt = 0; attempt < 2; attempt++)
                {
                    try
                    {
                        if (pipe == null || !pipe.IsConnected)
                        {
                            if (pipe != null) pipe.Dispose();
                            pipe = new NamedPipeClientStream(".", PipeName, PipeDirection.InOut);
                            pipe.Connect(250);
                            w = new StreamWriter(pipe, new UTF8Encoding(false)) { AutoFlush = true, NewLine = "\r\n" };
                            // the service replies to some messages; drain them so the pipe never blocks
                            var rp = pipe;
                            var t = new Thread(() => { try { var r = new StreamReader(rp); while (r.ReadLine() != null) { } } catch { } }) { IsBackground = true };
                            t.Start();
                        }
                        w.WriteLine(line);
                        break;
                    }
                    catch
                    {
                        try { if (pipe != null) pipe.Dispose(); } catch { }
                        pipe = null;
                        if (!_run) break;
                    }
                }
            }
            try { if (pipe != null) pipe.Dispose(); } catch { }
        }
    }
}
