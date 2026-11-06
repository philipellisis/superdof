using System;
using System.Collections.Concurrent;
using System.IO;
using System.IO.Pipes;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;

namespace DirectOutputCom
{
    /// <summary>
    /// Replacement for DirectOutput's COM object (ProgID DirectOutput.ComObject), which
    /// PinUP Popper (through PinUPDOF.DOFLink) and some table scripts create to send
    /// DOF events. It has the same CLSID and the same methods, but forwards the events
    /// over the named pipe "SuperDOF" to superdof.exe instead of hosting DOF.
    /// </summary>
    [ComVisible(true)]
    [Guid("A23BFDBC-9A8A-46C0-8672-60F23D54FFB6")]
    [ProgId("DirectOutput.ComObject")]
    [ClassInterface(ClassInterfaceType.AutoDual)]
    public class ComObject
    {
        private const string PipeName = "SuperDOF";
        private readonly BlockingCollection<string> _queue = new BlockingCollection<string>(8192);
        private Thread _worker;
        private string _host = "";

        public void Init(string HostingApplicationName, string TableFileName, string GameName)
        {
            _host = HostingApplicationName ?? "";
            if (_worker == null)
            {
                _worker = new Thread(Pump) { IsBackground = true, Name = "SuperDOF COM pipe" };
                _worker.Start();
            }
            _queue.TryAdd("INIT|" + Clean(TableFileName) + "|" + Clean(GameName));
        }

        public void Finish()
        {
            _queue.TryAdd("FINISH");
        }

        public void UpdateTableElement(string TableElementTypeChar, int Number, int Value)
        {
            if (string.IsNullOrEmpty(TableElementTypeChar)) return;
            _queue.TryAdd("D|" + TableElementTypeChar[0] + "|" + Number + "|" + Value);
        }

        public void UpdateNamedTableElement(string TableElementName, int Value)
        {
            if (string.IsNullOrEmpty(TableElementName)) return;
            string n = TableElementName.Trim();
            int num;
            // "E901" is a table element of type E; anything else is a named element
            if (n.Length > 1 && char.IsLetter(n[0]) && int.TryParse(n.Substring(1), out num))
                _queue.TryAdd("D|" + char.ToUpperInvariant(n[0]) + "|" + num + "|" + Value);
            else
                _queue.TryAdd("N|" + Clean(n.TrimStart('$')) + "|" + Value);
        }

        public string GetVersion() { return "SuperDOF COM 0.1"; }
        public string GetName() { return "SuperDOF"; }
        public string GetDllPath() { return typeof(ComObject).Assembly.Location; }
        public string TableMappingFileName() { return ""; }
        public void ShowFrontend() { }
        public string[] GetConfiguredTableElmentDescriptors() { return new string[0]; }

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
                            // replies are never read: a pending read on a synchronous pipe handle
                            // would block every later write
                        }
                        w.WriteLine(line);
                        break;
                    }
                    catch
                    {
                        try { if (pipe != null) pipe.Dispose(); } catch { }
                        pipe = null;
                    }
                }
            }
        }
    }
}
