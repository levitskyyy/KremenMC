package kremenmc.probe.spawn;

import java.io.*;
import java.nio.file.*;
import java.util.*;
import java.util.zip.InflaterInputStream;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.nbt.NbtAccounter;
import net.minecraft.nbt.NbtIo;
import net.minecraft.nbt.Tag;

// GenSeed runs a real dedicated server (separate JVM, loopback only,
// offline) on a fresh noise world with a fixed seed, stops it right after
// startup and prints every saved entity (id, position, yaw) from the entity
// region files. The Go side fingerprints creature yaws against the
// chunk-generation random to identify its seed.
// Args: seed.
public final class GenSeed {
    public static void main(String[] args) throws Exception {
        Env.stdout();
        var o = Env.out;
        Path cwd = Path.of("").toAbsolutePath();
        Path dir = Files.createTempDirectory(cwd, "spawnworld-genseed-");
        try {
            Files.writeString(dir.resolve("eula.txt"), "eula=true\n");
            int port = 20000 + new Random().nextInt(20000);
            Files.writeString(dir.resolve("server.properties"), String.join("\n",
                "level-seed=" + args[0], "online-mode=false", "server-ip=127.0.0.1", "server-port=" + port,
                "generate-structures=false", "view-distance=6", "simulation-distance=4", "spawn-protection=0",
                "enable-query=false", "enable-rcon=false", "difficulty=peaceful", "sync-chunk-writes=true", "") );
            ProcessBuilder pb = new ProcessBuilder(Path.of(System.getProperty("java.home"), "bin", "java").toString(), "-Xmx3g",
                "-cp", System.getProperty("java.class.path"), "net.minecraft.server.Main", "--nogui");
            pb.directory(dir.toFile());
            pb.redirectErrorStream(true);
            Process p = pb.start();
            var in = new BufferedReader(new InputStreamReader(p.getInputStream()));
            var out = new PrintWriter(p.getOutputStream(), true);
            String line;
            boolean done = false;
            while ((line = in.readLine()) != null) {
                System.err.println(line);
                if (!done && line.contains("Done (")) {
                    done = true;
                    if (args.length < 3) out.println("tick freeze");
                    out.println("forceload add -120 -120 119 119");
                    new Thread(() -> {
                        try {
                            Thread.sleep(Long.parseLong(args.length > 1 ? args[1] : "20000"));
                        } catch (InterruptedException ignored) {
                        }
                        out.println("save-all flush");
                        out.println("stop");
                    }).start();
                }
            }
            p.waitFor();
            o.println("seed " + args[0] + " started=" + done);
            List<Path> files = new ArrayList<>();
            try (var s = Files.walk(dir)) {
                s.filter(f -> f.toString().endsWith(".mca") && (f.getParent().getFileName().toString().equals("entities") || f.getParent().getFileName().toString().equals("region"))
                    && !f.toString().contains("the_nether") && !f.toString().contains("the_end") && !f.toString().contains("DIM")).sorted().forEach(files::add);
            }

            for (Path f : files) dumpRegion(f, o);
        } finally {
            Env.deleteTree(dir);
        }
        o.flush();
    }

    static void dumpRegion(Path f, PrintStream o) throws IOException {
        byte[] b = Files.readAllBytes(f);
        if (b.length < 8192) return;
        for (int i = 0; i < 1024; i++) {
            int off = ((b[i * 4] & 0xff) << 16) | ((b[i * 4 + 1] & 0xff) << 8) | (b[i * 4 + 2] & 0xff);
            if (off == 0) continue;
            int pos = off * 4096;
            int len = ((b[pos] & 0xff) << 24) | ((b[pos + 1] & 0xff) << 16) | ((b[pos + 2] & 0xff) << 8) | (b[pos + 3] & 0xff);
            int comp = b[pos + 4];
            if (comp != 2) throw new IOException("compression " + comp);
            var data = new DataInputStream(new InflaterInputStream(new ByteArrayInputStream(b, pos + 5, len - 1)));
            CompoundTag tag = NbtIo.read(data, NbtAccounter.unlimitedHeap());

            int[] cp = tag.getIntArray("Position").orElse(new int[] {tag.getIntOr("xPos", 0), tag.getIntOr("zPos", 0)});
            var list = tag.getListOrEmpty("Entities");
            if (list.isEmpty()) list = tag.getListOrEmpty("entities");
            o.println("chunk " + cp[0] + " " + cp[1] + " " + tag.getStringOr("Status", "?"));
            for (Tag t : list) {
                CompoundTag e = (CompoundTag) t;
                var ps = e.getListOrEmpty("Pos");
                var rot = e.getListOrEmpty("Rotation");
                o.println("entity " + e.getStringOr("id", "?") + " " + cp[0] + " " + cp[1] + " " + Rec.d(ps.getDoubleOr(0, 0)) + " " + Rec.d(ps.getDoubleOr(1, 0))
                    + " " + Rec.d(ps.getDoubleOr(2, 0)) + " " + Rec.f(rot.getFloatOr(0, 0)));
            }
        }
    }
}
