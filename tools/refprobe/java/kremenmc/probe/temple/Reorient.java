package kremenmc.probe.temple;

import java.util.HashMap;
import java.util.Map;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.Registries;
import net.minecraft.world.level.BlockGetter;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.levelgen.structure.StructurePiece;

// Reorient calls StructurePiece.reorient (public static) as a black box on
// random neighbour configurations through a proxy BlockGetter that logs
// every read. Prints "CASE <input facing> <N> <E> <S> <W> -> <facing>
// reads <dirs>". Args: n seed.
public final class Reorient {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        String[] pal = {"air", "stone", "chest", "glass", "oak_leaves", "oak_slab", "tripwire", "cobblestone", "water", "oak_stairs", "dirt", "torch"};
        String[] facings = {"north", "east", "south", "west"};
        BlockState[] ps = new BlockState[pal.length];
        for (int i = 0; i < pal.length; i++)
            ps[i] = net.minecraft.core.registries.BuiltInRegistries.BLOCK.getValue(net.minecraft.resources.Identifier.parse("minecraft:" + pal[i])).defaultBlockState();
        int n = Integer.parseInt(args[0]);
        java.util.Random r = new java.util.Random(Long.parseLong(args[1]));
        BlockPos c = new BlockPos(0, 64, 0);
        for (int k = 0; k < n; k++) {
            Map<BlockPos, BlockState> m = new HashMap<>();
            int[] pick = new int[4];
            BlockPos[] nb = {c.north(), c.east(), c.south(), c.west()};
            for (int i = 0; i < 4; i++) {
                pick[i] = r.nextInt(3) == 0 ? 0 : r.nextInt(pal.length);
                m.put(nb[i], ps[pick[i]]);
            }
            StringBuilder reads = new StringBuilder();
            BlockGetter g = (BlockGetter) java.lang.reflect.Proxy.newProxyInstance(Reorient.class.getClassLoader(), new Class<?>[]{BlockGetter.class}, (proxy, meth, a) -> {
                if (meth.getName().equals("getBlockState")) {
                    BlockPos p = (BlockPos) a[0];
                    String d = p.equals(nb[0]) ? "N" : p.equals(nb[1]) ? "E" : p.equals(nb[2]) ? "S" : p.equals(nb[3]) ? "W" : p.toShortString();
                    reads.append(d);
                    return m.getOrDefault(p, ps[0]);
                }
                if (meth.getName().equals("getFluidState")) return m.getOrDefault((BlockPos) a[0], ps[0]).getFluidState();
                if (meth.getName().equals("getMinY")) return -64;
                if (meth.getName().equals("getHeight")) return 384;
                if (meth.isDefault()) return java.lang.reflect.InvocationHandler.invokeDefault(proxy, meth, a);
                return null;
            });
            String f0 = facings[r.nextInt(4)];
            BlockState in = ps[2].setValue(net.minecraft.world.level.block.ChestBlock.FACING, net.minecraft.core.Direction.byName(f0));
            BlockState out = StructurePiece.reorient(g, c, in);
            System.out.println("CASE " + f0 + " " + pal[pick[0]] + " " + pal[pick[1]] + " " + pal[pick[2]] + " " + pal[pick[3]] + " -> " + out.getValue(net.minecraft.world.level.block.ChestBlock.FACING).getName() + " reads " + reads);
        }
    }
}
