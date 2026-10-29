package kremenmc.probe.legacy;

import net.minecraft.world.level.biome.BiomeManager;

// Zoom asks BiomeManager.getBiome for block positions and prints the quart
// it reads from its source. Args: <seed> <x> <y> <z> ...
public final class Zoom {
    public static void main(String[] args) {
        java.io.PrintStream out = System.out;
        long seed = BiomeManager.obfuscateSeed(Long.parseLong(args[0]));
        for (int i = 1; i + 2 < args.length; i += 3) {
            int x = Integer.parseInt(args[i]), y = Integer.parseInt(args[i + 1]), z = Integer.parseInt(args[i + 2]);
            final int[] q = new int[3];
            BiomeManager bm = new BiomeManager((qx, qy, qz) -> { q[0] = qx; q[1] = qy; q[2] = qz; return null; }, seed);
            bm.getBiome(new net.minecraft.core.BlockPos(x, y, z));
            out.println(x + " " + y + " " + z + " -> " + q[0] + " " + q[1] + " " + q[2]);
        }
    }
}
