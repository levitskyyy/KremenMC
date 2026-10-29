package kremenmc.probe.legacy;

import java.io.PrintStream;
import net.minecraft.SharedConstants;
import net.minecraft.core.Direction;
import net.minecraft.world.level.levelgen.structure.BoundingBox;
import net.minecraft.world.level.levelgen.structure.StructurePiece;
import net.minecraft.world.level.levelgen.structure.structures.MineshaftPieces;
import net.minecraft.world.level.levelgen.structure.structures.MineshaftStructure;

// MineParts black-boxes the mineshaft piece finders and addChildren in
// isolation: for each direction and random seed it calls findCorridorSize,
// findCrossing and findStairs at 0,50,0 with an empty builder, and builds
// a piece there and runs its addChildren (genDepth 1) with the logging
// builder, printing every draw, collision query and added piece.
public final class MineParts {
    public static void main(String[] args) throws Exception {
        PrintStream out = new PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), true);
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        MineLog.out = out;
        LogWR.OUT = out;
        int seeds = Integer.parseInt(args[0]);
        var T = MineshaftStructure.Type.NORMAL;
        for (Direction d : new Direction[]{Direction.NORTH, Direction.EAST, Direction.SOUTH, Direction.WEST}) {
            for (int s = 0; s < seeds; s++) {
                for (String kind : new String[]{"corridor", "crossing", "stairs"}) {
                    LogWR r = new LogWR();
                    r.setSeed(s * 0x9E3779B97F4A7C15L + 12345L);
                    r.on = true;
                    var b = new MineLog.LogBuilder();
                    out.println("CASE " + kind + " " + d + " seed " + s);
                    BoundingBox bb = switch (kind) {
                        case "corridor" -> MineshaftPieces.MineShaftCorridor.findCorridorSize(b, r, 0, 50, 0, d);
                        case "crossing" -> MineshaftPieces.MineShaftCrossing.findCrossing(b, r, 0, 50, 0, d);
                        default -> MineshaftPieces.MineShaftStairs.findStairs(b, r, 0, 50, 0, d);
                    };
                    out.println("BOX " + (bb == null ? "null" : MineLog.box(bb)));
                    if (bb == null) continue;
                    StructurePiece p = switch (kind) {
                        case "corridor" -> new MineshaftPieces.MineShaftCorridor(1, r, bb, d, T);
                        case "crossing" -> new MineshaftPieces.MineShaftCrossing(1, bb, d, T);
                        default -> new MineshaftPieces.MineShaftStairs(1, bb, d, T);
                    };
                    b.addPiece(p);
                    out.println("CHILDREN");
                    p.addChildren(p, b, r);
                }
            }
        }
    }
}
