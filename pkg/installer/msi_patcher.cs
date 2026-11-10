using System;
using System.IO;
using System.Runtime.InteropServices;

namespace Linbeam
{
    public static class MsiPatcher
    {
        private static readonly IntPtr MSIDBOPEN_READONLY = (IntPtr)0;
        private static readonly IntPtr MSIDBOPEN_TRANSACT = (IntPtr)1;

        [DllImport("msi.dll", CharSet = CharSet.Unicode)]
        private static extern uint MsiOpenDatabase(string szDatabasePath, IntPtr szPersist, out IntPtr phDatabase);

        [DllImport("msi.dll", CharSet = CharSet.Unicode)]
        private static extern uint MsiDatabaseOpenView(IntPtr hDatabase, string szQuery, out IntPtr phView);

        [DllImport("msi.dll", CharSet = CharSet.Unicode)]
        private static extern uint MsiViewExecute(IntPtr hView, IntPtr hRecord);

        [DllImport("msi.dll", CharSet = CharSet.Unicode)]
        private static extern uint MsiViewFetch(IntPtr hView, out IntPtr phRecord);

        [DllImport("msi.dll")]
        private static extern uint MsiRecordReadStream(IntPtr hRecord, uint iField, byte[] szDataBuf, ref int pcbDataBuf);

        [DllImport("msi.dll")]
        private static extern uint MsiDatabaseCommit(IntPtr hDatabase);

        [DllImport("msi.dll")]
        private static extern uint MsiCloseHandle(IntPtr hAny);

        private static bool ExecuteSql(IntPtr hDb, string sql)
        {
            IntPtr hView = IntPtr.Zero;
            try
            {
                uint rc = MsiDatabaseOpenView(hDb, sql, out hView);
                if (rc != 0)
                {
                    return false;
                }
                rc = MsiViewExecute(hView, IntPtr.Zero);
                return rc == 0;
            }
            finally
            {
                if (hView != IntPtr.Zero)
                {
                    MsiCloseHandle(hView);
                }
            }
        }

        private static void ExtractSetupFile(IntPtr hDb, string setupFileKey, string outputPath)
        {
            IntPtr hView = IntPtr.Zero;
            IntPtr hRecord = IntPtr.Zero;
            try
            {
                string sql = "SELECT `Stream` FROM `ISSetupFile` WHERE `ISSetupFile`='" + setupFileKey + "'";
                if (MsiDatabaseOpenView(hDb, sql, out hView) != 0)
                {
                    return;
                }
                if (MsiViewExecute(hView, IntPtr.Zero) != 0)
                {
                    return;
                }
                if (MsiViewFetch(hView, out hRecord) != 0 || hRecord == IntPtr.Zero)
                {
                    return;
                }
                int size = 0;
                MsiRecordReadStream(hRecord, 1, null, ref size);
                if (size > 0)
                {
                    byte[] buffer = new byte[size];
                    if (MsiRecordReadStream(hRecord, 1, buffer, ref size) == 0)
                    {
                        File.WriteAllBytes(outputPath, buffer);
                        Console.WriteLine("Extracted " + setupFileKey + " -> " + outputPath + " (" + size + " bytes)");
                    }
                }
            }
            catch (Exception ex)
            {
                Console.WriteLine("Warning extracting " + setupFileKey + ": " + ex.Message);
            }
            finally
            {
                if (hRecord != IntPtr.Zero) MsiCloseHandle(hRecord);
                if (hView != IntPtr.Zero) MsiCloseHandle(hView);
            }
        }

        public static int Main(string[] args)
        {
            if (args.Length < 1)
            {
                Console.Error.WriteLine("Usage: msi_patcher.exe <path-to-msi> [extract-dir]");
                return 1;
            }

            string msiPath = args[0];
            string extractDir = args.Length > 1 ? args[1] : Path.GetDirectoryName(msiPath);

            IntPtr hDb = IntPtr.Zero;
            uint rc = MsiOpenDatabase(msiPath, MSIDBOPEN_TRANSACT, out hDb);
            if (rc != 0 || hDb == IntPtr.Zero)
            {
                Console.Error.WriteLine("MsiOpenDatabase failed with code: " + rc);
                return 2;
            }

            try
            {
                if (!string.IsNullOrEmpty(extractDir) && Directory.Exists(extractDir))
                {
                    ExtractSetupFile(hDb, "SetupFile1", Path.Combine(extractDir, "Bluebeam Admin Install.exe"));
                    ExtractSetupFile(hDb, "SetupFile2", Path.Combine(extractDir, "Bluebeam Admin Install.exe.config"));
                    ExtractSetupFile(hDb, "SetupFile3", Path.Combine(extractDir, "Bluebeam Plugins Admin Install.exe"));
                    ExtractSetupFile(hDb, "SetupFile4", Path.Combine(extractDir, "Bluebeam Plugins Admin Install.exe.config"));
                }

                string[] actionsToRemove = new string[]
                {
                    "Set_REBOOT_NEEDED_Prop_PS",
                    "Set_LANGUAGE_CHOICE_Prop_PS",
                    "CloseRevu_PS",
                    "KillBbPrint_PS",
                    "ExpandWebView2",
                    "Call_LaunchShellExplorer",
                    "RunBbPrint",
                    "RunBBPrint_ARM64"
                };

                foreach (string action in actionsToRemove)
                {
                    ExecuteSql(hDb, "DELETE FROM `InstallExecuteSequence` WHERE `Action`='" + action + "'");
                    ExecuteSql(hDb, "DELETE FROM `InstallUISequence` WHERE `Action`='" + action + "'");
                }

                // Add msidbCustomActionTypeContinue (0x40 = 64) to InstallShield custom actions
                // so non-zero return codes in Wine do not trigger an MSI rollback.
                ExecuteSql(hDb, "UPDATE `CustomAction` SET `Type`=65 WHERE `Action`='PreInstall'");
                ExecuteSql(hDb, "UPDATE `CustomAction` SET `Type`=65 WHERE `Action`='PostInstall'");
                ExecuteSql(hDb, "UPDATE `CustomAction` SET `Type`=1665 WHERE `Action`='InvokeGeneralAdmin'");
                ExecuteSql(hDb, "UPDATE `CustomAction` SET `Type`=1665 WHERE `Action`='InvokeGeneralAdminPlugins'");
                ExecuteSql(hDb, "UPDATE `CustomAction` SET `Type`=1666 WHERE `Action`='Call_LaunchShellExplorer'");

                // Remove LaunchConditions that fail false-positively under Wine/Proton
                ExecuteSql(hDb, "DELETE FROM `LaunchCondition`");

                // Ensure default properties required by removed PowerShell actions are present
                ExecuteSql(hDb, "UPDATE `Property` SET `Value`='1' WHERE `Property`='IGNORE_RBT'");
                if (!ExecuteSql(hDb, "INSERT INTO `Property` (`Property`, `Value`) VALUES ('LANGUAGE_CHOICE', 'en')"))
                {
                    ExecuteSql(hDb, "UPDATE `Property` SET `Value`='en' WHERE `Property`='LANGUAGE_CHOICE'");
                }

                uint commitRc = MsiDatabaseCommit(hDb);
                if (commitRc != 0)
                {
                    Console.Error.WriteLine("MsiDatabaseCommit failed with code: " + commitRc);
                    return 3;
                }

                Console.WriteLine("Successfully patched MSI database for Wine compatibility: " + msiPath);
                return 0;
            }
            finally
            {
                if (hDb != IntPtr.Zero)
                {
                    MsiCloseHandle(hDb);
                }
            }
        }
    }
}
