package net.minecraft.world.scores;

public enum DisplaySlot {
    LIST("list"),
    SIDEBAR("sidebar"),
    BELOW_NAME("below_name"),
    TEAM_BLACK("sidebar.team.black"),
    TEAM_DARK_BLUE("sidebar.team.dark_blue"),
    TEAM_DARK_GREEN("sidebar.team.dark_green"),
    TEAM_DARK_AQUA("sidebar.team.dark_aqua"),
    TEAM_DARK_RED("sidebar.team.dark_red"),
    TEAM_DARK_PURPLE("sidebar.team.dark_purple"),
    TEAM_GOLD("sidebar.team.gold"),
    TEAM_GRAY("sidebar.team.gray"),
    TEAM_DARK_GRAY("sidebar.team.dark_gray"),
    TEAM_BLUE("sidebar.team.blue"),
    TEAM_GREEN("sidebar.team.green"),
    TEAM_AQUA("sidebar.team.aqua"),
    TEAM_RED("sidebar.team.red"),
    TEAM_LIGHT_PURPLE("sidebar.team.light_purple"),
    TEAM_YELLOW("sidebar.team.yellow"),
    TEAM_WHITE("sidebar.team.white");

    private final String name;

    DisplaySlot(String name) { this.name = name; }

    public int id() { return ordinal(); }
    public String getSerializedName() { return name; }

    public static DisplaySlot byId(int id) {
        DisplaySlot[] v = values();
        return id >= 0 && id < v.length ? v[id] : LIST;
    }
}
