package net.minecraft.world.scores;

public enum TeamColor {
    BLACK, DARK_BLUE, DARK_GREEN, DARK_AQUA, DARK_RED, DARK_PURPLE, GOLD, GRAY,
    DARK_GRAY, BLUE, GREEN, AQUA, RED, LIGHT_PURPLE, YELLOW, WHITE;

    public int getId() { return ordinal(); }

    public static TeamColor byId(int id) {
        TeamColor[] v = values();
        return id >= 0 && id < v.length ? v[id] : WHITE;
    }
}
