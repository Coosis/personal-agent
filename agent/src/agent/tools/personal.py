"""Personal-information tool definitions."""

from langchain_core.tools import tool


def create_personal_information_tool():
    """Create the personal-information tool."""

    @tool
    def get_personal_information() -> str:
        """Retrieve user profile information for questions about the user."""
        return "User is a computer science student at HHU university junior year."

    return get_personal_information
