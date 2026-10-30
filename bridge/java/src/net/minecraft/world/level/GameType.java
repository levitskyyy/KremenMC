package net.minecraft.world.level;

public enum GameType {
    SURVIVAL(0, "survival"),
    CREATIVE(1, "creative"),
    ADVENTURE(2, "adventure"),
    SPECTATOR(3, "spectator");

    public static final GameType DEFAULT_MODE = SURVIVAL;

    private final int id;
    private final String name;

    GameType(int id, String name) {
        this.id = id;
        this.name = name;
    }

    public int getId() { return id; }
    public String getName() { return name; }
    public String getSerializedName() { return name; }

    public static GameType byId(int id) {
        for (GameType t : values()) if (t.id == id) return t;
        return DEFAULT_MODE;
    }

    public static GameType byName(String name) { return byName(name, DEFAULT_MODE); }

    public static GameType byName(String name, GameType fallback) {
        for (GameType t : values()) if (t.name.equals(name)) return t;
        return fallback;
    }
}
