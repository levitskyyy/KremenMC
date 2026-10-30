package kremenmc.probe.oruin;

import net.minecraft.world.phys.shapes.BitSetDiscreteVoxelShape;

// FaceOrder prints DiscreteVoxelShape.forAllFaces for random voxel sets:
// "SHAPE sx sy sz" then the filled voxels "V x y z", then "F dir x y z" in
// call order. Args: count seed.
public final class FaceOrder {
    public static void main(String[] args) {
        int n = Integer.parseInt(args[0]);
        java.util.Random r = new java.util.Random(Long.parseLong(args[1]));
        for (int i = 0; i < n; i++) {
            int sx = 1 + r.nextInt(5), sy = 1 + r.nextInt(5), sz = 1 + r.nextInt(5);
            var s = new BitSetDiscreteVoxelShape(sx, sy, sz);
            System.out.println("SHAPE " + sx + " " + sy + " " + sz);
            for (int x = 0; x < sx; x++) for (int y = 0; y < sy; y++) for (int z = 0; z < sz; z++) if (r.nextInt(3) != 0) { s.fill(x, y, z); System.out.println("V " + x + " " + y + " " + z); }
            s.forAllFaces((d, x, y, z) -> System.out.println("F " + d + " " + x + " " + y + " " + z));
        }
    }
}
